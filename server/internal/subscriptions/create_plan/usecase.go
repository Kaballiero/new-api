package create_plan

import "github.com/QuantumNous/new-api/internal/subscriptions/domain"

// Store executes the original plan write; UpdatePlan retains its single-write transaction.
type Store interface {
	InsertPlan(plan *domain.SubscriptionPlan) error
}
type Usecase struct {
	store      Store
	rules      domain.PlanRules
	invalidate func(planID int)
}

func NewUsecase(store Store, rules domain.PlanRules, invalidate func(planID int)) Usecase {
	return Usecase{store: store, rules: rules, invalidate: invalidate}
}
func (u Usecase) Execute(plan domain.SubscriptionPlan) (domain.SubscriptionPlan, error) {
	if err := domain.NormalizePlan(&plan, true, u.rules); err != nil {
		return domain.SubscriptionPlan{}, err
	}
	if err := u.store.InsertPlan(&plan); err != nil {
		return domain.SubscriptionPlan{}, err
	}
	u.invalidate(plan.Id)
	return plan, nil
}
