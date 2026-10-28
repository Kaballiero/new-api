package cleanup_preconsume

type Store interface {
	Cleanup(cutoff int64) (int64, error)
}
type Usecase struct {
	store Store
	now   func() int64
}

func NewUsecase(store Store, now func() int64) Usecase { return Usecase{store, now} }
func (u Usecase) Execute(olderThanSeconds int64) (int64, error) {
	if olderThanSeconds <= 0 {
		olderThanSeconds = 7 * 24 * 3600
	}
	return u.store.Cleanup(u.now() - olderThanSeconds)
}
