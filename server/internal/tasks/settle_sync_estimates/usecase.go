package settle_sync_estimates

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/shopspring/decimal"
)

// Store pages manual holds and the holds already settled at an estimate,
// claims an estimate settlement on the revision transaction an operator
// decision uses, and checks that it can apply.
type Store interface {
	ManualHolds(afterID int64, limit int) ([]*domain.Task, error)
	EstimateSettledHolds(afterFinishTime, afterID int64, limit int) ([]*domain.Task, error)
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

const pageSize = 100

// settlementRetryWindow is how long after its claim an estimate settlement
// that moved no balances is retried; afterwards root repeats the decision.
const settlementRetryWindow = time.Hour

func NewUsecase(store Store, settlement Settlement, projection Projection, diagnostics Diagnostics, now func() time.Time) Usecase {
	return Usecase{store, settlement, projection, diagnostics, now}
}

// Execute retries the claimed estimate settlements that moved no balances,
// then settles, at an estimate, every new manual hold whose result was
// delivered without an upstream cost. A hold that cannot settle stays with
// root.
func (u Usecase) Execute(ctx context.Context) error {
	now := u.now()
	if err := u.retry(ctx, now); err != nil {
		return err
	}
	var afterID int64
	for ctx.Err() == nil {
		holds, err := u.store.ManualHolds(afterID, pageSize)
		if err != nil {
			return err
		}
		for _, task := range holds {
			afterID = task.ID
			u.settle(ctx, task, now)
		}
		if len(holds) < pageSize {
			break
		}
	}
	return ctx.Err()
}

// retry applies again the claimed estimate settlements of the retry window
// whose balances did not move; one that moved them is never replayed.
func (u Usecase) retry(ctx context.Context, now time.Time) error {
	afterFinishTime, afterID := now.Add(-settlementRetryWindow).Unix(), int64(0)
	for ctx.Err() == nil {
		holds, err := u.store.EstimateSettledHolds(afterFinishTime, afterID, pageSize)
		if err != nil {
			return err
		}
		for _, task := range holds {
			afterFinishTime, afterID = task.FinishTime, task.ID
			if r := task.PrivateData.Reconciliation; !r.SettlementPending || r.MainApplied {
				continue
			}
			if err := u.settlement.SettleDurable(task); err != nil {
				u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s estimate settlement retry incomplete: %v", task.TaskID, err))
				continue
			}
			u.diagnostics.Info(ctx, fmt.Sprintf("OpenRouter sync task %s estimate settlement applied on retry", task.TaskID))
		}
		if len(holds) < pageSize {
			break
		}
	}
	return ctx.Err()
}

// settle records an automatic settle decision at the estimate and applies it.
// Only a hold not yet reported to root is tried, so root keeps every hold it
// was told about and a declined hold is tried once.
func (u Usecase) settle(ctx context.Context, task *domain.Task, now time.Time) {
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if !task.OpenRouterSyncOperatorEligible(domain.OpenRouterSyncOperatorSettle) || a.AlertedAt != 0 {
		return
	}
	// The decision stores the cost as a decimal string the claim compares with
	// the priced cost, so the estimate is rounded once before pricing.
	estimate := decimal.NewFromFloat(domain.OpenRouterSyncEstimateUSD(task)).Round(12)
	if !estimate.IsPositive() {
		return
	}
	estimateUSD, costUSD := estimate.InexactFloat64(), estimate.String()
	if err := task.PriceOpenRouterSyncDecision(estimateUSD, u.settlement.Prepare); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s left to root: estimate %s USD cannot settle: %v", task.TaskID, costUSD, err))
		return
	}
	if r.TargetQuota <= 0 || r.TargetQuota > r.ReservedQuota {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s left to root: estimate quota %d is outside the reserve %d", task.TaskID, r.TargetQuota, r.ReservedQuota))
		return
	}
	if err := u.store.Feasible(task); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s left to root: estimate settlement is not feasible: %v", task.TaskID, err))
		return
	}
	r.UpstreamCostEstimated = true
	task.RecordOpenRouterSyncDecision(domain.OpenRouterSyncOperatorDecision{Action: domain.OpenRouterSyncOperatorSettle, CostUSD: costUSD,
		Evidence: "automatic estimate: result delivered without an upstream cost", DecidedAt: now.Unix()},
		domain.SyncReasonEstimateSettled, domain.SyncEstimateSettledFailReason)
	// The estimate charge finishes the task, so the summary and the retry
	// select it by its finish time.
	task.FinishTime = now.Unix()
	u.projection.Project(task)
	// A lost revision claim means another writer resolved the hold first.
	if err := u.store.UpdateRevision(task, a.Revision); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s estimate settlement deferred: %v", task.TaskID, err))
		return
	}
	if err := u.settlement.SettleDurable(task); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s estimate settlement of %s USD incomplete; the next pass retries it: %v", task.TaskID, costUSD, err))
		return
	}
	u.diagnostics.Info(ctx, fmt.Sprintf("OpenRouter sync task %s settled by estimate: cost_usd=%s target_quota=%d reserved_quota=%d", task.TaskID, costUSD, r.TargetQuota, r.ReservedQuota))
}
