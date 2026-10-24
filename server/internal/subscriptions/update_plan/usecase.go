package update_plan

import "github.com/QuantumNous/new-api/internal/subscriptions/domain"

// Store executes the original plan write; UpdatePlan retains its single-write transaction.
type Store interface {
	UpdatePlan(plan domain.SubscriptionPlan) error
}
type Input struct {
	ID   int
	Plan domain.SubscriptionPlan
}
type Usecase struct {
	store      Store
	rules      domain.PlanRules
	invalidate func(planID int)
}

func NewUsecase(store Store, rules domain.PlanRules, invalidate func(planID int)) Usecase {
	return Usecase{store: store, rules: rules, invalidate: invalidate}
}
func (u Usecase) Execute(in Input) error {
	if in.ID <= 0 {
		return domain.ErrInvalidID
	}
	in.Plan.Id = in.ID

	if err := domain.NormalizePlan(&in.Plan, false, u.rules); err != nil {
		return err
	}
	if err := u.store.UpdatePlan(in.Plan); err != nil {
		return err
	}
	u.invalidate(in.Plan.Id)
	return nil
}
