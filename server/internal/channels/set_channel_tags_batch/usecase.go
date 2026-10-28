package set_channel_tags_batch

type Store interface{ SetTag([]int, *string) error }
type Usecase struct {
	store   Store
	refresh func()
	audit   func(int)
}

func NewUsecase(store Store, refresh func(), audit func(int)) Usecase {
	return Usecase{store: store, refresh: refresh, audit: audit}
}
func (usecase Usecase) Execute(input Input) (int, error) {
	if err := usecase.store.SetTag(input.Ids, input.Tag); err != nil {
		return 0, err
	}
	usecase.refresh()
	usecase.audit(len(input.Ids))
	return len(input.Ids), nil
}
