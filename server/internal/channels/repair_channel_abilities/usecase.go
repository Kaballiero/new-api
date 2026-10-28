package repair_channel_abilities

type Store interface{ RepairAbilities() (int, int, error) }
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (usecase Usecase) Execute() (Output, error) {
	success, fails, err := usecase.store.RepairAbilities()
	return Output{Success: success, Fails: fails}, err
}
