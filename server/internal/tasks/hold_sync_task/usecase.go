package hold_sync_task

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Store interface{ UpdateRevision(*domain.Task, int) error }
type Projection interface{ Project(*domain.Task) }
type Input struct {
	Task             *domain.Task
	IdentityConflict bool
	Reason           string
}
type Usecase struct {
	store      Store
	projection Projection
	now        func() time.Time
}

func NewUsecase(store Store, projection Projection, now func() time.Time) Usecase {
	return Usecase{store, projection, now}
}
func (u Usecase) Execute(_ context.Context, input Input) error {
	task := input.Task
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if !r.SubmissionPending || u.now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter synchronous result lease expired")
	}
	a.IdentityConflict = a.IdentityConflict || input.IdentityConflict
	r.SubmissionPending = false
	a.ResultFailed = true
	a.RecordResultReason(input.Reason)
	domain.MarkSyncManual(task, input.Reason, u.now().Unix())
	u.projection.Project(task)
	return u.store.UpdateRevision(task, a.Revision)
}
