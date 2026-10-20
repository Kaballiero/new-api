package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisabledTaskArtifactStoreHasNoStorageBehavior(t *testing.T) {
	store := GetTaskArtifactStore()
	require.NotNil(t, store)
	assert.False(t, store.Enabled())

	task := &model.Task{TaskID: "task-disabled-store"}
	ref, err := store.Resolve(task, "video")
	require.NoError(t, err)
	assert.Nil(t, ref)

	ref, err = store.Persist(t.Context(), task, types.TaskArtifact{Key: "video", Type: "video"}, strings.NewReader("content"))
	assert.Nil(t, ref)
	assert.ErrorIs(t, err, ErrTaskArtifactStoreDisabled)
	assert.ErrorIs(t, store.Serve(&gin.Context{}, task, &StoredArtifactRef{Backend: "s3"}), ErrTaskArtifactStoreDisabled)
	assert.Same(t, store, GetTaskArtifactStore())
}

var _ TaskArtifactStore = disabledArtifactStore{}

func TestFilesystemTaskArtifactSurvivesReopenAndVerifiesSnapshot(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0700))
	store, err := newFilesystemTaskArtifactStore(directory)
	require.NoError(t, err)
	task := &model.Task{TaskID: "task-audio-result", Status: model.TaskStatusInProgress}
	payload := "raw audio result"
	artifact := types.TaskArtifact{Key: "result", Type: "audio", MimeType: "audio/pcm"}
	ref, err := store.Persist(t.Context(), task, artifact, strings.NewReader(payload))
	require.NoError(t, err)
	task.PrivateData.OpenRouterAudio = &model.OpenRouterAudioTask{ResultReady: true, ResultSize: ref.Size, ResultSHA256: ref.SHA256, ResultMimeType: ref.MimeType}
	info, err := os.Stat(filepath.Join(directory, ref.ObjectKey))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	_, err = store.Persist(t.Context(), task, artifact, strings.NewReader("replacement"))
	require.Error(t, err, "published results must never be overwritten")
	require.NoError(t, store.root.Close())
	store, err = newFilesystemTaskArtifactStore(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.root.Close()) })
	resolved, err := store.Resolve(task, "result")
	require.NoError(t, err)
	require.Equal(t, ref, resolved)
	for _, status := range []model.TaskStatus{model.TaskStatusInProgress, model.TaskStatusFailure, model.TaskStatusSuccess} {
		task.Status = status
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(method, "/artifact", nil)
			require.NoError(t, store.Serve(c, task, resolved))
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, "audio/pcm", recorder.Header().Get("Content-Type"))
			assert.Equal(t, "16", recorder.Header().Get("Content-Length"))
			if method == http.MethodGet {
				assert.Equal(t, payload, recorder.Body.String())
			} else {
				assert.Empty(t, recorder.Body.String())
			}
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(directory, ref.ObjectKey), []byte("RAW AUDIO RESULT"), 0600))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(method, "/artifact", nil)
		require.ErrorContains(t, store.Serve(c, task, resolved), "checksum mismatch")
		assert.Empty(t, recorder.Body.String())
		assert.Empty(t, recorder.Header().Get("Content-Type"))
	}
}

func TestFilesystemTaskArtifactRejectsUntrustedPathsAndOversize(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0700))
	store, err := newFilesystemTaskArtifactStore(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.root.Close()) })
	artifact := types.TaskArtifact{Key: "result", Type: "file", MimeType: "application/json"}
	for _, taskID := range []string{"../outside", "/absolute", "..", "a\\b"} {
		_, err := store.Persist(t.Context(), &model.Task{TaskID: taskID}, artifact, strings.NewReader("private"))
		require.Error(t, err)
	}
	task := &model.Task{TaskID: "oversize"}
	_, err = store.Persist(t.Context(), task, artifact, io.LimitReader(repeatingArtifactReader{}, TaskArtifactMaxBytes+1))
	require.ErrorContains(t, err, "exceeds size limit")
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	assert.Empty(t, entries, "oversize failure must not publish or retain temporary bytes")
	outside := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(outside, []byte("private"), 0600))
	key, err := taskArtifactObjectKey(task, "result")
	require.NoError(t, err)
	require.NoError(t, os.Symlink(outside, filepath.Join(directory, key)))
	_, err = store.Persist(t.Context(), task, artifact, bytes.NewReader([]byte("safe")))
	require.Error(t, err)
	contents, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "private", string(contents))
}

type repeatingArtifactReader struct{}

func (repeatingArtifactReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
