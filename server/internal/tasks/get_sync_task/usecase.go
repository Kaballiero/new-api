package get_sync_task

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Reader resolves one public identifier without choosing an arbitrary tenant.
type Reader interface {
	Unique(taskID string) (*domain.Task, bool, error)
}
type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader: reader} }

func (u Usecase) Execute(_ context.Context, input domain.SyncIdentity) (*domain.Task, error) {
	if input.TaskID == "" {
		return nil, errors.New("missing durable OpenRouter synchronous task")
	}
	task, exists, err := u.reader.Unique(input.TaskID)
	if err != nil {
		return nil, err
	}
	platform, action, err := input.Operation()
	if err != nil {
		return nil, err
	}
	if !exists || task.PrivateData.OpenRouterSyncState() == nil || task.Platform != platform || task.Action != action ||
		task.UserId != input.UserID ||
		task.ChannelId != input.ChannelID ||
		task.PrivateData.TokenId != input.TokenID ||
		task.Properties.UpstreamModelName != input.UpstreamModel ||
		task.PrivateData.OpenRouterSyncState().RequestID != input.RequestID ||
		(platform != string(domain.TaskPlatformOpenRouterAudio)) != (task.PrivateData.OpenRouterSync != nil) ||
		task.PrivateData.OpenRouterSync != nil && task.PrivateData.OpenRouterAudio != nil ||
		input.NativeImage && task.PrivateData.OpenRouterSyncState().ProviderTag != input.ProviderTag {
		return nil, errors.New("OpenRouter synchronous task identity changed")
	}
	return task, nil
}
