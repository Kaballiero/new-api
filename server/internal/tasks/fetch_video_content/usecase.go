package fetch_video_content

import (
	"context"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type ContentProvider interface {
	BuildContentRequest(task *domain.Task, key string, client domain.MediaClientRequest) (*domain.MediaRequest, bool, error)
}
type Diagnostics interface {
	QueryError(ctx context.Context, taskID string, err error)
	ContentWarning(ctx context.Context, taskID string)
	ProjectionWarning(ctx context.Context, taskID string)
}
type Input struct {
	TaskID string
	Access domain.TaskAccess
	Client domain.MediaClientRequest
}
type Result struct {
	Task       *domain.Task
	Descriptor *domain.MediaRequest
}
type Usecase struct {
	reader      domain.TaskAccessReader
	projector   domain.ArtifactProjector
	provider    ContentProvider
	diagnostics Diagnostics
}

func NewUsecase(
	reader domain.TaskAccessReader,
	projector domain.ArtifactProjector,
	provider ContentProvider,
	diagnostics Diagnostics,
) Usecase {
	return Usecase{reader: reader, projector: projector, provider: provider, diagnostics: diagnostics}
}
func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	if in.TaskID == "" {
		return Result{}, domain.ErrVideoTaskIDRequired
	}
	task, exists, err := domain.ResolveTaskAccess(u.reader, in.Access, in.TaskID)
	if err != nil {
		u.diagnostics.QueryError(ctx, in.TaskID, err)
		return Result{}, domain.ErrTaskQuery
	}
	if !exists || task == nil {
		return Result{}, domain.ErrTaskNotFound
	}
	if task.Status != domain.TaskStatusSuccess {
		return Result{}, &domain.TaskIncompleteError{Status: task.Status}
	}
	var descriptor *domain.MediaRequest
	if domain.HasPluginExecution(task) {
		artifacts, err := domain.ProjectArtifacts(task, u.projector)
		if err != nil {
			u.diagnostics.ProjectionWarning(ctx, in.TaskID)
		} else {
			for _, artifact := range artifacts {
				if artifact.Type != "video" {
					continue
				}
				descriptor, _, err = u.provider.BuildContentRequest(task, artifact.Key, in.Client)
				if err != nil {
					u.diagnostics.ContentWarning(ctx, in.TaskID)
					descriptor = nil
				}
				break
			}
		}
	}
	if descriptor == nil {
		resultURL := task.GetResultURL()
		if domain.IsMediaFallbackLoop(resultURL, task.TaskID) {
			return Result{}, domain.ErrArtifactGone
		}
		descriptor = &domain.MediaRequest{URL: resultURL, Method: in.Client.Method, Credentialless: true}
	}
	return Result{Task: task, Descriptor: descriptor}, nil
}
