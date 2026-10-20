package update_channel_status

type Store interface{ ChangeStatus(int, int, string) bool }
type Usecase struct {
	store   Store
	refresh func()
	audit   func(Input, bool)
}

func NewUsecase(store Store, refresh func(), audit func(Input, bool)) Usecase {
	return Usecase{store: store, refresh: refresh, audit: audit}
}
func (usecase Usecase) Execute(input Input) bool {
	changed := usecase.store.ChangeStatus(input.ID, input.Status, "manual operation")
	if changed {
		usecase.refresh()
	}
	usecase.audit(input, changed)
	return changed
}
