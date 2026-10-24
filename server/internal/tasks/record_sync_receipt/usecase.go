package record_sync_receipt

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Store interface {
	UpdateRevision(task *domain.Task, revision int) error
}
type Projection interface{ Project(task *domain.Task) }
type Input struct {
	Task                 *domain.Task
	ResponsePresent      bool
	Status               int
	RequestError         bool
	RequestIDs           []string
	GenerationID, APIKey string
	GenerationConflict   bool
	Observe              func(generationID string, conflict bool)
}
type Usecase struct {
	store Store
	codec Projection
	now   func() time.Time
}

func NewUsecase(store Store, codec Projection, now func() time.Time) Usecase {
	return Usecase{store: store, codec: codec, now: now}
}

func (u Usecase) Execute(_ context.Context, input Input) error {
	task := input.Task
	a := task.PrivateData.OpenRouterSyncState()
	image := task.Platform == string(domain.TaskPlatformOpenRouterImage) || task.Platform == string(domain.TaskPlatformOpenRouterChatImage)
	if u.now().Unix() >= a.LeaseUntil && !image {
		return errors.New("OpenRouter synchronous submission lease expired")
	}
	if image && (!a.DispatchPrepared || task.PrivateData.Reconciliation.ReservationPending) {
		return errors.New("OpenRouter image dispatch was not prepared")
	}
	if input.ResponsePresent {
		if image {
			for _, id := range input.RequestIDs {
				if domain.ValidOpenRouterImageCorrelation(id, input.APIKey, 128) {
					a.UpstreamRequestID = id
					break
				}
			}
		}
		id := input.GenerationID
		if task.Platform == string(domain.TaskPlatformOpenRouterChatImage) && id != "" {
			a.IdentityConflict = a.IdentityConflict || input.GenerationConflict || !domain.ValidOpenRouterImageCorrelation(id, input.APIKey, 128)
			if a.IdentityConflict {
				input.Observe("", true)
				id = ""
			}
		}
		if id != "" && len(id) <= 512 && (task.Platform != string(domain.TaskPlatformOpenRouterImage) ||
			domain.ValidOpenRouterImageCorrelation(id, input.APIKey, 512)) {
			if task.Platform == string(domain.TaskPlatformOpenRouterChatImage) &&
				task.PrivateData.UpstreamTaskID != "" &&
				task.PrivateData.UpstreamTaskID != id {
				a.IdentityConflict = true
				input.Observe("", true)
				return u.store.UpdateRevision(task, a.Revision)
			}
			task.PrivateData.UpstreamTaskID = id
			input.Observe(id, false)
		}
	}
	if image && task.Status == domain.TaskStatusFailure {
		return u.store.UpdateRevision(task, a.Revision)
	}
	if input.RequestError || !input.ResponsePresent || input.Status != 200 && !image {
		task.PrivateData.Reconciliation.SubmissionPending = false
		a.ResultFailed, a.LastError = true, "submission_outcome_unknown"
		a.NextPoll = u.now().Unix()
		if task.PrivateData.UpstreamTaskID == "" || task.Platform == string(domain.TaskPlatformOpenRouterImage) {
			domain.MarkSyncManual(task, "submission_outcome_unknown", u.now().Unix())
		}
	}
	u.codec.Project(task)
	return u.store.UpdateRevision(task, a.Revision)
}
