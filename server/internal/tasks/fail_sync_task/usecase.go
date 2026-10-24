package fail_sync_task

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
type Settlement interface {
	Prepare(task *domain.Task)
	SettleDurable(task *domain.Task) error
}
type Input struct {
	Task   *domain.Task
	Cost   domain.SyncCost
	Reason string
}
type Usecase struct {
	store      Store
	codec      Projection
	settlement Settlement
	now        func() time.Time
}

func NewUsecase(store Store, codec Projection, settlement Settlement, now func() time.Time) Usecase {
	return Usecase{store: store, codec: codec, settlement: settlement, now: now}
}

func (u Usecase) Execute(_ context.Context, input Input) error {
	task, state := input.Task, input.Cost
	a := task.PrivateData.OpenRouterSyncState()
	a.IdentityConflict = a.IdentityConflict || state.IdentityConflict
	if id := state.GenerationID; id != "" && len(id) <= 512 {
		task.PrivateData.UpstreamTaskID = id
	}
	r := task.PrivateData.Reconciliation
	lateImageCost := (task.Platform == string(domain.TaskPlatformOpenRouterImage) ||
		task.Platform == string(domain.TaskPlatformOpenRouterChatImage)) && state.Final && state.Valid && !a.IdentityConflict && !r.ReservationPending && !r.HasUpstreamCost && !r.SettlementPending && !r.Finalized
	if (!r.SubmissionPending || u.now().Unix() >= a.LeaseUntil) && !lateImageCost {
		return errors.New("OpenRouter synchronous result lease expired")
	}
	a.ResultFailed, a.LastError = true, input.Reason
	r.SubmissionPending = false
	a.NextPoll = u.now().Unix()
	if state.Final && state.Valid && !a.IdentityConflict && (task.Platform == string(domain.TaskPlatformOpenRouterImage) ||
		task.Platform == string(domain.TaskPlatformOpenRouterChatImage)) {
		r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, state.CostUSD
		domain.FinishSyncTask(task, u.now().Unix(), u.settlement.Prepare)
	} else if a.IdentityConflict || task.PrivateData.UpstreamTaskID == "" || task.Platform == string(domain.TaskPlatformOpenRouterImage) {
		domain.MarkSyncManual(task, input.Reason, u.now().Unix())
	}
	u.codec.Project(task)
	if err := u.store.UpdateRevision(task, a.Revision); err != nil {
		return err
	}
	if r.SettlementPending {
		return u.settlement.SettleDurable(task)
	}
	return nil
}
