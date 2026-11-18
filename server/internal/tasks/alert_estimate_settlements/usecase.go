package alert_estimate_settlements

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store pages the tasks that may carry an estimate charge or claim by
// (finish_time, id), marks the reported estimate charges and the stalled
// claims reported to root.
type Store interface {
	EstimateCharges(afterFinishTime, afterID int64, limit int) ([]*domain.Task, error)
	MarkEstimateAlerted(task *domain.Task, alertedAt int64) error
	MarkAlerted(task *domain.Task, alertedAt int64) error
}

// Alerts delivers one summary of estimate charges and stalled estimate claims
// to the operators.
type Alerts interface {
	EstimateSettlements(charges, stalledClaims []*domain.Task) error
}
type Diagnostics interface {
	Warn(ctx context.Context, message string)
}
type Usecase struct {
	store       Store
	alerts      Alerts
	diagnostics Diagnostics
	now         func() time.Time
	// windowStart is the start of the alert window of the previous run, so a
	// charge that left the window unreported since then is logged once.
	windowStart *int64
}

const pageSize = 100

// estimateAlertWindow is how long after a task finished its unreported
// estimate charge is still collected, which bounds the retries of a summary
// that was not delivered.
const estimateAlertWindow = time.Hour

func NewUsecase(store Store, alerts Alerts, diagnostics Diagnostics, now func() time.Time) Usecase {
	return Usecase{store, alerts, diagnostics, now, new(int64)}
}

// Execute reports every estimate charge of the alert window not reported
// before, and every estimate claim of the window whose balances have not moved
// for domain.SyncSettlementStallAge, in one summary and marks them reported. A
// summary that is not delivered leaves them for the next run; a failed summary
// never undoes a charge. One that leaves the window unreported is logged.
func (u Usecase) Execute(ctx context.Context) error {
	now := u.now()
	windowStart := now.Add(-estimateAlertWindow).Unix()
	afterFinishTime, afterID := windowStart, int64(0)
	if previous := *u.windowStart; previous != 0 && previous < windowStart {
		afterFinishTime = previous
	}
	var unreported, stalledClaims, agedOut []*domain.Task
	for ctx.Err() == nil {
		tasks, err := u.store.EstimateCharges(afterFinishTime, afterID, pageSize)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			afterFinishTime, afterID = task.FinishTime, task.ID
			// A claim records its finish time, so its age is the time its
			// balances have not moved.
			stalledClaim := task.FailReason == domain.SyncEstimatePendingFailReason && task.PrivateData.OpenRouterSyncState().AlertedAt == 0 &&
				now.Sub(time.Unix(task.FinishTime, 0)) >= domain.SyncSettlementStallAge
			r := task.PrivateData.Reconciliation
			charge := r != nil && r.UpstreamCostEstimated && r.MainApplied && r.EstimateAlertedAt == 0
			if !stalledClaim && !charge {
				continue
			}
			switch {
			case task.FinishTime < windowStart:
				agedOut = append(agedOut, task)
			case stalledClaim:
				stalledClaims = append(stalledClaims, task)
			default:
				unreported = append(unreported, task)
			}
		}
		if len(tasks) < pageSize {
			break
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	*u.windowStart = windowStart
	if len(agedOut) > 0 {
		taskIDs := make([]string, len(agedOut))
		for i, task := range agedOut {
			taskIDs[i] = task.TaskID
		}
		u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter estimate charges and stalled claims left the alert window unreported: %s", strings.Join(taskIDs, ", ")))
	}
	if len(unreported)+len(stalledClaims) == 0 {
		return nil
	}
	if err := u.alerts.EstimateSettlements(unreported, stalledClaims); err != nil {
		return err
	}
	alertedAt := now.Unix()
	for _, task := range unreported {
		if err := u.store.MarkEstimateAlerted(task, alertedAt); err != nil {
			return err
		}
	}
	for _, task := range stalledClaims {
		if err := u.store.MarkAlerted(task, alertedAt); err != nil {
			return err
		}
	}
	return nil
}
