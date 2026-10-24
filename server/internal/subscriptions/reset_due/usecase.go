package reset_due

import (
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

type Store interface {
	DueCandidates(limit int, now int64) ([]domain.UserSubscription, error)
	ResetPlan(int) (*domain.SubscriptionPlan, error)
	ResetDue(func(domain.DueResetTx) error) error
}
type Usecase struct {
	store Store
	now   func() int64
}

func NewUsecase(store Store, now func() int64) Usecase { return Usecase{store, now} }
func (u Usecase) Execute(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := u.now()
	subs, err := u.store.DueCandidates(limit, now)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, sub := range subs {
		plan, err := u.store.ResetPlan(sub.PlanId)
		if err != nil || plan == nil {
			continue
		}
		err = u.store.ResetDue(func(tx domain.DueResetTx) error {
			locked, err := tx.LockDueSubscription(sub.Id, now)
			if err != nil {
				return nil
			}
			changed, err := domain.ResetProgress(locked, plan, now)
			if err != nil {
				return err
			}
			if changed {
				if err := tx.SaveSubscription(locked); err != nil {
					return err
				}
			}
			count++
			return nil
		})
		if err != nil {
			return count, err
		}
	}
	return count, nil
}
