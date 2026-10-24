package complete_sync_task

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

// Diagnostics reports repair without coupling delivery success to post-commit effects.
type Diagnostics interface {
	SettlementRepair(ctx context.Context, taskID string)
}
type Input struct {
	Task     *domain.Task
	Cost     domain.SyncCost
	BodySize int64
	MimeType string
}
type Usecase struct {
	store       Store
	codec       Projection
	settlement  Settlement
	diagnostics Diagnostics
	now         func() time.Time
}

func NewUsecase(store Store, codec Projection, settlement Settlement, diagnostics Diagnostics, now func() time.Time) Usecase {
	return Usecase{store: store, codec: codec, settlement: settlement, diagnostics: diagnostics, now: now}
}

func (u Usecase) Execute(ctx context.Context, input Input) error {
	task, state := input.Task, input.Cost
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	lateChatCost := task.Platform == string(domain.TaskPlatformOpenRouterChatImage) &&
		state.Final &&
		state.Valid &&
		!state.IdentityConflict &&
		!r.ReservationPending &&
		!r.HasUpstreamCost &&
		!r.SettlementPending &&
		!r.Finalized
	if (u.now().Unix() >= a.LeaseUntil || !r.SubmissionPending) && !lateChatCost {
		return errors.New("OpenRouter synchronous result lease expired")
	}
	a.IdentityConflict = a.IdentityConflict || state.IdentityConflict
	if a.IdentityConflict {
		return errors.New("OpenRouter generation identity is unverified")
	}
	if state.GenerationID != "" && len(state.GenerationID) <= 512 {
		task.PrivateData.UpstreamTaskID = state.GenerationID
	}
	if !state.Final && task.PrivateData.UpstreamTaskID == "" && task.Platform != string(domain.TaskPlatformOpenRouterChatImage) {
		return errors.New("OpenRouter synchronous cost and generation identity are unavailable")
	}
	a.ResultReady, a.ResultMimeType, a.ResultSize = true, input.MimeType, input.BodySize
	r.SubmissionPending = false
	a.NextPoll = u.now().Unix()
	if state.Final && state.Valid {
		r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, state.CostUSD
		r.UpstreamIsBYOK, r.UpstreamCostDetails = state.IsBYOK, string(state.CostDetails)
		domain.FinishSyncTask(task, u.now().Unix(), u.settlement.Prepare)
	} else if task.PrivateData.UpstreamTaskID == "" {
		domain.MarkSyncManual(task, "missing_generation_identity", u.now().Unix())
	}
	u.codec.Project(task)
	if err := u.store.UpdateRevision(task, a.Revision); err != nil {
		return err
	}
	if r.SettlementPending {
		if err := u.settlement.SettleDurable(task); err != nil {
			u.diagnostics.SettlementRepair(ctx, task.TaskID)
		}
	}
	return nil
}
