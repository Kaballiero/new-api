package settle_sync_estimates

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/shopspring/decimal"
)

// Store pages manual holds and the finished tasks that can carry an estimate
// charge, claims an estimate settlement on the revision transaction an
// operator decision uses, and marks the reported estimate charges.
type Store interface {
	ManualHolds(afterID int64, limit int) ([]*domain.Task, error)
	Feasible(*domain.Task) error
	UpdateRevision(*domain.Task, int) error
	FinishedEstimateTasks(since, afterID int64, limit int) ([]*domain.Task, error)
	MarkEstimateAlerted(task *domain.Task, alertedAt int64) error
}
type Settlement interface {
	Prepare(*domain.Task)
	SettleDurable(*domain.Task) error
}
type Projection interface{ Project(*domain.Task) }

// Alerts delivers one summary of estimate charges to the operators.
type Alerts interface {
	EstimateSettlements(tasks []*domain.Task) error
}
type Diagnostics interface {
	Info(ctx context.Context, message string)
	Warn(ctx context.Context, message string)
}
type Usecase struct {
	store       Store
	settlement  Settlement
	projection  Projection
	alerts      Alerts
	diagnostics Diagnostics
	now         func() time.Time
}

const pageSize = 100

// estimateAlertWindow is how long after a task finished its unreported
// estimate charge is still collected, which bounds the retries of a summary
// that was not delivered.
const estimateAlertWindow = time.Hour

func NewUsecase(store Store, settlement Settlement, projection Projection, alerts Alerts, diagnostics Diagnostics, now func() time.Time) Usecase {
	return Usecase{store, settlement, projection, alerts, diagnostics, now}
}

// Execute settles, at an estimate, every new manual hold whose result was
// delivered without an upstream cost, then reports every estimate charge not
// reported before in one summary. A hold that cannot settle stays with root.
func (u Usecase) Execute(ctx context.Context) error {
	now := u.now()
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
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return u.report(ctx, now)
}

// settle records an automatic settle decision at the estimate and applies it.
// Only a hold not yet reported to root is tried, so root keeps every hold it
// was told about and a declined hold is tried once.
func (u Usecase) settle(ctx context.Context, task *domain.Task, now time.Time) {
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if !task.OpenRouterSyncOperatorEligible(domain.OpenRouterSyncOperatorSettle) || a.AlertedAt != 0 {
		return
	}
	estimateUSD := domain.OpenRouterSyncEstimateUSD(task)
	if estimateUSD <= 0 {
		return
	}
	domain.RecordOpenRouterCostEstimate(task, estimateUSD)
	if err := task.PriceOpenRouterSyncDecision(estimateUSD, u.settlement.Prepare); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s left to root: estimate cannot settle: %v", task.TaskID, err))
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
	costUSD := decimal.NewFromFloat(estimateUSD).String()
	task.RecordOpenRouterSyncDecision(domain.OpenRouterSyncOperatorDecision{Action: domain.OpenRouterSyncOperatorSettle, CostUSD: costUSD,
		Evidence: "automatic estimate: result delivered without an upstream cost", DecidedAt: now.Unix()},
		domain.SyncReasonEstimateSettled, "OpenRouter cost settled by estimate")
	// The estimate charge finishes the task, so the summary collects it.
	task.FinishTime = now.Unix()
	u.projection.Project(task)
	// A lost revision claim means another writer resolved the hold first.
	if err := u.store.UpdateRevision(task, a.Revision); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s estimate settlement deferred: %v", task.TaskID, err))
		return
	}
	if err := u.settlement.SettleDurable(task); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter sync task %s estimate settlement incomplete; root must repeat the settle decision: %v", task.TaskID, err))
		return
	}
	u.diagnostics.Info(ctx, fmt.Sprintf("OpenRouter sync task %s settled by estimate: cost_usd=%s target_quota=%d reserved_quota=%d", task.TaskID, costUSD, r.TargetQuota, r.ReservedQuota))
}

// report sends one summary of the estimate charges not reported before and
// marks them reported. A summary that is not delivered leaves them for the
// next run within the alert window; a failed summary never undoes a charge.
func (u Usecase) report(ctx context.Context, now time.Time) error {
	var unreported []*domain.Task
	since := now.Add(-estimateAlertWindow).Unix()
	var afterID int64
	for ctx.Err() == nil {
		tasks, err := u.store.FinishedEstimateTasks(since, afterID, pageSize)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			afterID = task.ID
			if r := task.PrivateData.Reconciliation; r != nil && r.UpstreamCostEstimated && r.MainApplied && r.EstimateAlertedAt == 0 {
				unreported = append(unreported, task)
			}
		}
		if len(tasks) < pageSize {
			break
		}
	}
	if ctx.Err() != nil || len(unreported) == 0 {
		return ctx.Err()
	}
	if err := u.alerts.EstimateSettlements(unreported); err != nil {
		return err
	}
	alertedAt := u.now().Unix()
	for _, task := range unreported {
		if err := u.store.MarkEstimateAlerted(task, alertedAt); err != nil {
			return err
		}
	}
	return nil
}
