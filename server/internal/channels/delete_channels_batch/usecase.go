package delete_channels_batch

type Store interface{ DeleteBatch([]int) (int64, error) }
type Usecase struct {
	store   Store
	refresh func()
	reset   func()
	audit   func(int64)
}

func NewUsecase(store Store, refresh func(), reset func(), audit func(int64)) Usecase {
	return Usecase{store: store, refresh: refresh, reset: reset, audit: audit}
}
func (usecase Usecase) Execute(input Input) (int64, error) {
	rows, err := usecase.store.DeleteBatch(input.Ids)
	if err != nil {
		return 0, err
	}
	usecase.refresh()
	if rows > 0 {
		usecase.reset()
	}
	usecase.audit(rows)
	return rows, nil
}
