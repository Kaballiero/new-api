package release_unidentified_sync_holds

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store pages unknown-submission holds, claims a release on the same revision
// transaction an operator decision uses, and re-parks holds it may not release.
type Store interface {
	UnknownSubmissionHolds(submittedBefore, afterID int64, limit int) ([]*domain.Task, error)
	ParkUnknownSubmissionHold(*domain.Task) error
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
// unknown without an upstream identity for the auto-release delay. A due hold
// that does not qualify or cannot settle is re-parked once for root.
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
			u.resolve(ctx, task, now)
		}
		if len(tasks) < holdPageSize {
			break
		}
	}
	return ctx.Err()
}

func (u Usecase) resolve(ctx context.Context, task *domain.Task, now time.Time) {
	reservedQuota := task.PrivateData.Reconciliation.ReservedQuota
	unavailable := u.priceRelease(task, now)
	if unavailable != nil {
		if err := u.store.ParkUnknownSubmissionHold(task); err != nil {
			u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s automatic release park failed: %v", task.TaskID, err))
			return
		}
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s left to root: automatic release unavailable: %v", task.TaskID, unavailable))
		return
	}
	u.projection.Project(task)
	// A lost revision claim means another writer resolved the hold first.
	if err := u.store.UpdateRevision(task, task.PrivateData.OpenRouterSyncState().Revision); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s automatic release deferred: %v", task.TaskID, err))
		return
	}
	if err := u.settlement.SettleDurable(task); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s automatic release settlement incomplete; root must repeat the release: %v", task.TaskID, err))
		return
	}
	u.diagnostics.Info(ctx, fmt.Sprintf("OpenRouter sync task %s reservation released automatically: released_quota=%d", task.TaskID, reservedQuota))
}

// priceRelease records a zero-cost release decision in memory, or reports why
// the hold may not be released automatically.
func (u Usecase) priceRelease(task *domain.Task, now time.Time) error {
	if !task.OpenRouterSyncAutoReleaseDue(now) {
		return fmt.Errorf("hold does not qualify")
	}
	if err := task.PriceOpenRouterSyncDecision(0, u.settlement.Prepare); err != nil {
		return err
	}
	if err := u.store.Feasible(task); err != nil {
		return fmt.Errorf("settlement is not feasible: %w", err)
	}
	task.RecordOpenRouterSyncDecision(domain.OpenRouterSyncOperatorDecision{Action: domain.OpenRouterSyncOperatorRelease, CostUSD: "0",
		Evidence: "automatic release: submission outcome unknown without upstream identity", DecidedAt: now.Unix()},
		"auto_released", "OpenRouter reservation released automatically: submission outcome unknown")
	return nil
}
