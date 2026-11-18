package refuse_sync_reservation

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store persists one revision claim on the task's existing transaction boundary.
type Store interface {
	UpdateRevision(task *domain.Task, revision int) error
}
type Projection interface{ Project(task *domain.Task) }
type Usecase struct {
	store Store
	codec Projection
	now   func() time.Time
}

func NewUsecase(store Store, codec Projection, now func() time.Time) Usecase {
	return Usecase{store: store, codec: codec, now: now}
}

// Execute closes an intent whose quota reservation was refused, or whose
// Commit failed before recording the reserved quota while the billing session
// still holds it for the caller to refund. Commit records the reserved quota
// before it transfers the hold, so an intent still pending without one leaves
// the Task nothing to reconcile.
func (u Usecase) Execute(_ context.Context, task *domain.Task) error {
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if !r.ReservationPending || r.ReservedQuota != 0 || u.now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter synchronous reservation is no longer refusable")
	}
	r.ReservationPending, r.Required, r.Finalized = false, false, true
	a.ResultFailed, a.LastError = true, domain.SyncReasonReservationRefused
	a.RecordResultReason(domain.SyncReasonReservationRefused)
	task.Status, task.Progress, task.FinishTime = domain.TaskStatusFailure, "100%", u.now().Unix()
	task.FailReason = "OpenRouter synchronous reservation refused; nothing was charged"
	u.codec.Project(task)
	return u.store.UpdateRevision(task, a.Revision)
}
