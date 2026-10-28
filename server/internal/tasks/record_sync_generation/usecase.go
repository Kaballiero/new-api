package record_sync_generation

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Store interface {
	UpdateRevision(task *domain.Task, revision int) error
}
type Input struct {
	Task                 *domain.Task
	GenerationID, APIKey string
	IdentityConflict     bool
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }

func (u Usecase) Execute(_ context.Context, input Input) error {
	task := input.Task
	if task.Platform != string(domain.TaskPlatformOpenRouterChatImage) {
		return errors.New("generation capture requires chat-image identity")
	}
	a := task.PrivateData.OpenRouterSyncState()
	if input.IdentityConflict ||
		!domain.ValidOpenRouterImageCorrelation(input.GenerationID, input.APIKey, 128) ||
		task.PrivateData.UpstreamTaskID != "" && task.PrivateData.UpstreamTaskID != input.GenerationID {
		a.IdentityConflict = true
	} else {
		task.PrivateData.UpstreamTaskID = input.GenerationID
	}
	if err := u.store.UpdateRevision(task, a.Revision); err != nil {
		return err
	}
	if a.IdentityConflict {
		return errors.New("OpenRouter generation identity changed")
	}
	return nil
}
