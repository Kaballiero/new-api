package commit_sync_reservation

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

// Hold transfers the already reserved amount and records its original consumption.
type Hold interface {
	Quota() int
	Transfer(quota int) error
	Record(task *domain.Task) error
}
type Input struct {
	Task           *domain.Task
	Source         string
	SubscriptionID int
	Hold           Hold
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
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if !r.ReservationPending || u.now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter synchronous reservation lease expired")
	}
	task.PrivateData.BillingSource, task.PrivateData.SubscriptionId = input.Source, input.SubscriptionID
	task.Quota = input.Hold.Quota()
	r.ReservedQuota = task.Quota
	revision := a.Revision
	u.codec.Project(task)
	if err := u.store.UpdateRevision(task, revision); err != nil {
		return err
	}
	if err := input.Hold.Transfer(task.Quota); err != nil {
		return err
	}
	if err := input.Hold.Record(task); err != nil {
		return err
	}
	r.ReservationPending, r.SubmissionPending = false, true
	task.Status, task.Progress, task.StartTime = domain.TaskStatusInProgress, "10%", u.now().Unix()
	u.codec.Project(task)
	return u.store.UpdateRevision(task, a.Revision)
}
