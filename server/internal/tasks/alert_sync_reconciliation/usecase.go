package alert_sync_reconciliation

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store pages the holds parked in manual reconciliation for root and marks
// the ones reported.
type Store interface {
	ManualHolds(afterID int64, limit int) ([]*domain.Task, error)
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

func NewUsecase(store Store, alerts Alerts, now func() time.Time) Usecase {
	return Usecase{store, alerts, now}
}

// Execute reports every manual hold not reported before in one summary, then
// marks those holds reported. A summary that is not delivered leaves them for
// the next run.
func (u Usecase) Execute(ctx context.Context) error {
	var unreported []*domain.Task
	var afterID int64
	for ctx.Err() == nil {
		holds, err := u.store.ManualHolds(afterID, holdPageSize)
		if err != nil {
			return err
		}
		for _, task := range holds {
			afterID = task.ID
			if task.PrivateData.OpenRouterSyncState().AlertedAt == 0 {
				unreported = append(unreported, task)
			}
		}
		if len(holds) < holdPageSize {
			break
		}
	}
	if ctx.Err() != nil || len(unreported) == 0 {
		return ctx.Err()
	}
	if err := u.alerts.ManualHolds(unreported); err != nil {
		return err
	}
	alertedAt := u.now().Unix()
	for _, task := range unreported {
		if err := u.store.MarkAlerted(task, alertedAt); err != nil {
			return err
		}
	}
	return nil
}
