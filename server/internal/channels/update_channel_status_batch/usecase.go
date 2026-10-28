package update_channel_status_batch

type Store interface{ ChangeStatus(int, int, string) bool }
type Usecase struct {
	store   Store
	refresh func()
	audit   func(Input, int)
}

func NewUsecase(store Store, refresh func(), audit func(Input, int)) Usecase {
	return Usecase{store: store, refresh: refresh, audit: audit}
}
func (usecase Usecase) Execute(input Input) int {
	count := 0
	for _, id := range input.Ids {
		if usecase.store.ChangeStatus(id, input.Status, "manual batch operation") {
			count++
		}
	}
	if count > 0 {
		usecase.refresh()
	}
	usecase.audit(input, count)
	return count
}
