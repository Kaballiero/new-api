package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// StoredArtifactRef describes a fully published immutable artifact object.
type StoredArtifactRef struct {
	Backend   string
	Bucket    string
	ObjectKey string
	MimeType  string
	Size      int64
	SHA256    string
}

// TaskArtifactStore is the persistence boundary for generated artifact bytes.
// types.TaskArtifact is re-exported by relay/channel as channel.TaskArtifact.
type TaskArtifactStore interface {
	Enabled() bool
	Resolve(task *model.Task, artifactKey string) (*StoredArtifactRef, error)
	Persist(ctx context.Context, task *model.Task, artifact types.TaskArtifact, content io.Reader) (*StoredArtifactRef, error)
	Serve(c *gin.Context, task *model.Task, ref *StoredArtifactRef) error
}

var ErrTaskArtifactStoreDisabled = errors.New("task artifact store is disabled")

type disabledArtifactStore struct{}

func (disabledArtifactStore) Enabled() bool {
	return false
}

func (disabledArtifactStore) Resolve(*model.Task, string) (*StoredArtifactRef, error) {
	return nil, nil
}

func (disabledArtifactStore) Persist(context.Context, *model.Task, types.TaskArtifact, io.Reader) (*StoredArtifactRef, error) {
	return nil, ErrTaskArtifactStoreDisabled
}

func (disabledArtifactStore) Serve(*gin.Context, *model.Task, *StoredArtifactRef) error {
	return ErrTaskArtifactStoreDisabled
}

var taskArtifactStore TaskArtifactStore = &disabledArtifactStore{}

func init() {
	if err := InitTaskArtifactStore(); err != nil {
		common.SysError("task artifact filesystem unavailable; storage disabled")
	}
}

// InitTaskArtifactStore configures storage during process startup only. Callers
// must not invoke it while requests or task polling are running.
func InitTaskArtifactStore() error {
	config := system_setting.LoadTaskArtifactStoreConfig()
	if previous, ok := taskArtifactStore.(*filesystemTaskArtifactStore); ok {
		if err := previous.root.Close(); err != nil {
			return err
		}
	}
	taskArtifactStore = &disabledArtifactStore{}
	if config.Mode != system_setting.TaskArtifactStoreModeFilesystem {
		return nil
	}
	store, err := newFilesystemTaskArtifactStore(config.Root)
	if err != nil {
		return err
	}
	taskArtifactStore = store
	return nil
}

// GetTaskArtifactStore returns the startup-configured storage backend.
func GetTaskArtifactStore() TaskArtifactStore {
	return taskArtifactStore
}

// TaskArtifactMaxBytes bounds both persisted and served synchronous results.
const TaskArtifactMaxBytes int64 = 64 << 20

type filesystemTaskArtifactStore struct{ root *os.Root }

func newFilesystemTaskArtifactStore(path string) (*filesystemTaskArtifactStore, error) {
	// Provisioning the durable volume is an operator responsibility. Never silently
	// create a missing mount as an ephemeral directory.
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil || info.Mode().Perm()&0077 != 0 {
		root.Close()
		return nil, errors.New("artifact root must be a private directory")
	}
	return &filesystemTaskArtifactStore{root: root}, nil
}

func (*filesystemTaskArtifactStore) Enabled() bool { return true }

func taskArtifactObjectKey(task *model.Task, key string) (string, error) {
	if task == nil || task.TaskID == "" || strings.TrimSpace(task.TaskID) != task.TaskID || strings.ContainsAny(task.TaskID, "/\\\x00") || task.TaskID == "." || task.TaskID == ".." || key != "result" {
		return "", errors.New("invalid task artifact identity")
	}
	digest := sha256.Sum256([]byte(task.TaskID))
	return hex.EncodeToString(digest[:]) + "-result", nil
}

func (s *filesystemTaskArtifactStore) Resolve(task *model.Task, key string) (*StoredArtifactRef, error) {
	objectKey, err := taskArtifactObjectKey(task, key)
	if err != nil {
		return nil, err
	}
	audio := task.PrivateData.OpenRouterSyncState()
	if audio == nil || !audio.ResultReady {
		return nil, nil
	}
	if audio.ResultSize < 0 || audio.ResultSize > TaskArtifactMaxBytes || len(audio.ResultSHA256) != sha256.Size*2 || len(audio.ResultMimeType) > 255 || strings.ContainsAny(audio.ResultMimeType, "\r\n") {
		return nil, errors.New("invalid task artifact descriptor")
	}
	if _, err := hex.DecodeString(audio.ResultSHA256); err != nil {
		return nil, errors.New("invalid task artifact checksum")
	}
	return &StoredArtifactRef{Backend: system_setting.TaskArtifactStoreModeFilesystem, ObjectKey: objectKey, MimeType: audio.ResultMimeType, Size: audio.ResultSize, SHA256: audio.ResultSHA256}, nil
}

func (s *filesystemTaskArtifactStore) Persist(ctx context.Context, task *model.Task, artifact types.TaskArtifact, content io.Reader) (*StoredArtifactRef, error) {
	objectKey, err := taskArtifactObjectKey(task, artifact.Key)
	if err != nil {
		return nil, err
	}
	if (artifact.Type != "audio" && artifact.Type != "file") || len(artifact.MimeType) > 255 || strings.ContainsAny(artifact.MimeType, "\r\n") {
		return nil, errors.New("invalid task artifact descriptor")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Exclusive temporary names and Link publish an immutable object atomically:
	// concurrent writers cannot replace an already-published result.
	tempKey := objectKey + "-" + common.GetUUID() + ".tmp"
	file, err := s.root.OpenFile(tempKey, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer s.root.Remove(tempKey)
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(content, TaskArtifactMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if size > TaskArtifactMaxBytes {
		return nil, errors.New("task artifact exceeds size limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := s.root.Link(tempKey, objectKey); err != nil {
		return nil, err
	}
	if err := s.root.Remove(tempKey); err != nil {
		return nil, err
	}
	directory, err := s.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return nil, err
	}
	return &StoredArtifactRef{Backend: system_setting.TaskArtifactStoreModeFilesystem, ObjectKey: objectKey, MimeType: artifact.MimeType, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (s *filesystemTaskArtifactStore) Serve(c *gin.Context, task *model.Task, ref *StoredArtifactRef) error {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return errors.New("unsupported artifact method")
	}
	expected, err := s.Resolve(task, "result")
	if err != nil {
		return err
	}
	if expected == nil || ref == nil || *expected != *ref {
		return errors.New("task artifact reference mismatch")
	}
	file, err := s.root.Open(expected.ObjectKey)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != expected.Size {
		return errors.New("task artifact size mismatch")
	}
	// Verify a bounded snapshot before writing any headers or bytes. This avoids
	// returning success (including HEAD) for a corrupted or externally changed file.
	body, err := io.ReadAll(io.LimitReader(file, TaskArtifactMaxBytes+1))
	if err != nil {
		return err
	}
	checksum := sha256.Sum256(body)
	if int64(len(body)) != expected.Size || hex.EncodeToString(checksum[:]) != expected.SHA256 {
		return errors.New("task artifact checksum mismatch")
	}
	c.Header("Content-Type", expected.MimeType)
	c.Header("Content-Length", fmt.Sprint(len(body)))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	if c.Request.Method == http.MethodGet {
		_, err = c.Writer.Write(body)
	}
	return err
}
