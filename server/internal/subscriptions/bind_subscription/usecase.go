package bind_subscription

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

// Store resolves the plan before the transaction and serializes grants on the user row.
type Store interface {
	PlanByID(planID int) (*domain.SubscriptionPlan, error)
	Bind(run func(domain.BindTx) error) error
}
type Input struct{ UserID, PlanID int }
type Result struct{ UpgradeGroup string }
type Usecase struct {
	store   Store
	now     func() int64
	refresh func(userID int, operation string)
}

func NewUsecase(store Store, now func() int64, refresh func(userID int, operation string)) Usecase {
	return Usecase{store: store, now: now, refresh: refresh}
}
func (u Usecase) Execute(in Input) (Result, error) {
	if in.UserID <= 0 || in.PlanID <= 0 {
		return Result{}, errors.New("invalid userId or planId")
	}
	plan, err := u.store.PlanByID(in.PlanID)
	if err != nil {
		return Result{}, err
	}
	changed := false
	err = u.store.Bind(func(tx domain.BindTx) error {
		if err := tx.LockUserID(in.UserID); err != nil {
			return err
		}
		sub, err := domain.GrantSubscription(tx, in.UserID, plan, "admin", u.now)
		if err == nil {
			changed = sub.PrevUserGroup != ""
		}
		return err
	})
	if err != nil {
		return Result{}, err
	}
	if changed {
		u.refresh(in.UserID, "admin subscription creation")
		return Result{UpgradeGroup: plan.UpgradeGroup}, nil
	}
	return Result{}, nil
}
