package alert_sync_reconciliation

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store pages the holds parked in manual reconciliation for root and the
// unfinished Tasks whose settlement may be pending, and marks the ones
// reported.
type Store interface {
	ManualHolds(afterID int64, limit int) ([]*domain.Task, error)
	Unfinished(afterID int64, limit int) ([]*domain.Task, error)
	MarkAlerted(task *domain.Task, alertedAt int64) error
}

// Alerts delivers one summary of manual holds to the operators.
type Alerts interface {
	ManualHolds(holds []*domain.Task) error
}

type Usecase struct {
	store  Store
	alerts Alerts
	now    func() time.Time
}

const holdPageSize = 100

// settlementStallAge is how long a settlement retried by the poller may stay
// pending before it is reported with the manual holds.
const settlementStallAge = 30 * time.Minute

func NewUsecase(store Store, alerts Alerts, now func() time.Time) Usecase {
	return Usecase{store, alerts, now}
}

// Execute reports every manual hold and every settlement
// pending longer than settlementStallAge not reported before in one summary,
// then marks those Tasks reported. A summary that is not delivered leaves
// them for the next run.
func (u Usecase) Execute(ctx context.Context) error {
	now := u.now()
	var unreported []*domain.Task
	for _, page := range []func(afterID int64, limit int) ([]*domain.Task, error){u.store.ManualHolds, u.store.Unfinished} {
		var afterID int64
		for ctx.Err() == nil {
			tasks, err := page(afterID, holdPageSize)
			if err != nil {
				return err
			}
			for _, task := range tasks {
				afterID = task.ID
				a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
				if a == nil || r == nil || a.AlertedAt != 0 {
					continue
				}
				manualHold := task.Status == domain.TaskStatusFailure
				stalledSettlement := r.SettlementPending && now.Sub(time.Unix(a.SettlementPendingSince, 0)) >= settlementStallAge
				if manualHold || stalledSettlement {
					unreported = append(unreported, task)
				}
			}
			if len(tasks) < holdPageSize {
				break
			}
		}
	}
	if ctx.Err() != nil || len(unreported) == 0 {
		return ctx.Err()
	}
	if err := u.alerts.ManualHolds(unreported); err != nil {
		return err
	}
	alertedAt := now.Unix()
	for _, task := range unreported {
		if err := u.store.MarkAlerted(task, alertedAt); err != nil {
			return err
		}
	}
	return nil
}
