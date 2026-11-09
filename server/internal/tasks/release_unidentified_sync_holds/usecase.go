package release_unidentified_sync_holds

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store pages unknown-submission holds and claims a release on the same
// revision transaction an operator decision uses.
type Store interface {
	UnknownSubmissionHolds(submittedBefore, afterID int64, limit int) ([]*domain.Task, error)
	Feasible(*domain.Task) error
	UpdateRevision(*domain.Task, int) error
}
type Settlement interface {
	Prepare(*domain.Task)
	SettleDurable(*domain.Task) error
}
type Projection interface{ Project(*domain.Task) }
type Diagnostics interface {
	Info(ctx context.Context, message string)
	Warn(ctx context.Context, message string)
}
type Usecase struct {
	store       Store
	settlement  Settlement
	projection  Projection
	diagnostics Diagnostics
	now         func() time.Time
}

const holdPageSize = 100

func NewUsecase(store Store, settlement Settlement, projection Projection, diagnostics Diagnostics, now func() time.Time) Usecase {
	return Usecase{store, settlement, projection, diagnostics, now}
}

// Execute releases, at zero cost, every hold whose submission outcome stayed
// unknown without an upstream identity for the auto-release delay.
func (u Usecase) Execute(ctx context.Context) error {
	now := u.now()
	submittedBefore := now.Add(-domain.OpenRouterSyncAutoReleaseDelay).Unix()
	var afterID int64
	for ctx.Err() == nil {
		tasks, err := u.store.UnknownSubmissionHolds(submittedBefore, afterID, holdPageSize)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			afterID = task.ID
			if !task.OpenRouterSyncAutoReleaseDue(now) {
				continue
			}
			if err := u.release(ctx, task, now); err != nil {
				u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s automatic release deferred: %v", task.TaskID, err))
			}
		}
		if len(tasks) < holdPageSize {
			break
		}
	}
	return ctx.Err()
}

// release records a zero-cost release decision before any funds move, so a
// concurrent operator decision or settlement loses the revision claim.
func (u Usecase) release(ctx context.Context, task *domain.Task, now time.Time) error {
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	releasedQuota := r.ReservedQuota
	r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, 0
	u.settlement.Prepare(task)
	if err := u.store.Feasible(task); err != nil {
		return fmt.Errorf("settlement is not feasible: %w", err)
	}
	a.OperatorDecision = &domain.OpenRouterSyncOperatorDecision{Action: domain.OpenRouterSyncOperatorRelease, CostUSD: "0",
		Evidence: "automatic release: submission outcome unknown without upstream identity", DecidedAt: now.Unix()}
	a.LastError = "auto_released"
	task.FailReason = "OpenRouter reservation released automatically: submission outcome unknown"
	u.projection.Project(task)
	if err := u.store.UpdateRevision(task, a.Revision); err != nil {
		return err
	}
	if err := u.settlement.SettleDurable(task); err != nil {
		return err
	}
	u.diagnostics.Info(ctx, fmt.Sprintf("OpenRouter sync task %s reservation released automatically: released_quota=%d", task.TaskID, releasedQuota))
	return nil
}
