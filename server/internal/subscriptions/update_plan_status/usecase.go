package update_plan_status

// Store changes enabled even when the plan does not exist, preserving affected-row semantics.
type Store interface {
	SetPlanEnabled(planID int, enabled bool) error
}
type Input struct {
	PlanID  int
	Enabled bool
}
type Usecase struct {
	store      Store
	invalidate func(planID int)
}

func NewUsecase(store Store, invalidate func(planID int)) Usecase {
	return Usecase{store: store, invalidate: invalidate}
}
func (u Usecase) Execute(in Input) error {
	if err := u.store.SetPlanEnabled(in.PlanID, in.Enabled); err != nil {
		return err
	}
	u.invalidate(in.PlanID)
	return nil
}
