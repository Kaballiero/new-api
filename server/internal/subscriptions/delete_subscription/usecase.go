package delete_subscription

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

// Store keeps the row change and downgrade decision in the original transaction.
type Store interface {
	Change(run func(domain.ChangeTx) error) error
}
type Usecase struct {
	store   Store
	now     func() int64
	refresh func(userID int, operation string)
}

func NewUsecase(store Store, now func() int64, refresh func(userID int, operation string)) Usecase {
	return Usecase{store: store, now: now, refresh: refresh}
}
func (u Usecase) Execute(in int) (string, error) {
	if in <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := u.now()
	group := ""
	userID := 0
	err := u.store.Change(func(tx domain.ChangeTx) error {
		sub, err := tx.LockSubscription(in)
		if err != nil {
			return err
		}
		userID = sub.UserId
		target, err := domain.DowngradeGroup(tx, sub, now)
		if err != nil {
			return err
		}
		group = target
		if err := tx.DeleteSubscription(in); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if group != "" && userID > 0 {
		u.refresh(userID, "admin subscription deletion")
	}
	return group, nil
}
