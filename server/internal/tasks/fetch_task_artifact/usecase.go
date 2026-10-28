package fetch_task_artifact

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type ContentProvider interface {
	BuildContentRequest(task *domain.Task, key string, client domain.MediaClientRequest) (*domain.MediaRequest, bool, error)
}
type Storage interface {
	Resolve(task *domain.Task, key string) (*domain.StoredArtifactRef, error)
}
type Input struct {
	TaskID      string
	ArtifactKey string
	Access      domain.TaskAccess
	Client      domain.MediaClientRequest
}
type Result struct {
	Task            *domain.Task
	Stored          *domain.StoredArtifactRef
	Descriptor      *domain.MediaRequest
	RequiredStorage bool
}
type Usecase struct {
	reader    domain.TaskAccessReader
	projector domain.ArtifactProjector
	provider  ContentProvider
	storage   Storage
}

func NewUsecase(reader domain.TaskAccessReader, projector domain.ArtifactProjector, provider ContentProvider, storage Storage) Usecase {
	return Usecase{reader: reader, projector: projector, provider: provider, storage: storage}
}
func (u Usecase) Execute(in Input) (Result, error) {
	task, exists, err := domain.ResolveTaskAccess(u.reader, in.Access, in.TaskID)
	if err != nil {
		return Result{}, domain.ErrTaskQuery
	}
	if !exists || task == nil {
		return Result{}, domain.ErrArtifactNotFound
	}
	key := strings.TrimSpace(in.ArtifactKey)
	if !domain.ArtifactKeyPattern.MatchString(key) {
		return Result{}, domain.ErrArtifactNotFound
	}
	if sync := task.PrivateData.OpenRouterSyncState(); sync != nil {
		if !sync.ResultReady {
			return Result{}, domain.ErrArtifactsNotReady
		}
		if key != "result" || sync.ResultSHA256 == "" {
			return Result{}, domain.ErrArtifactNotFound
		}
		ref, err := u.storage.Resolve(task, key)
		if err != nil || ref == nil {
			return Result{}, domain.ErrArtifactStorageUnavailable
		}
		return Result{Task: task, Stored: ref, RequiredStorage: true}, nil
	}
	if task.Status != domain.TaskStatusSuccess {
		return Result{}, domain.ErrArtifactsNotReady
	}
	if !domain.HasPluginExecution(task) {
		if key != "video" || !domain.LegacyVideoAvailable(task) {
			return Result{}, domain.ErrArtifactNotFound
		}
		return Result{Task: task, Descriptor: &domain.MediaRequest{URL: task.GetResultURL(), Method: in.Client.Method, Credentialless: true}}, nil
	}
	artifacts, err := domain.ProjectArtifacts(task, u.projector)
	if err != nil {
		return Result{}, err
	}
	found := false
	for _, artifact := range artifacts {
		if artifact.Key == key {
			found = true
			break
		}
	}
	if !found {
		return Result{}, domain.ErrArtifactNotFound
	}
	if ref, err := u.storage.Resolve(task, key); err == nil && ref != nil {
		return Result{Task: task, Stored: ref}, nil
	}
	descriptor, supported, err := u.provider.BuildContentRequest(task, key, in.Client)
	if err != nil {
		return Result{}, err
	}
	if !supported {
		return Result{}, domain.ErrArtifactContentUnavailable
	}
	if descriptor == nil {
		return Result{}, domain.ErrArtifactContent
	}
	return Result{Task: task, Descriptor: descriptor}, nil
}
