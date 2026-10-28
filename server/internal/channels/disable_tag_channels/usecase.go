package disable_tag_channels

type Store interface{ DisableTag(string) error }
type Usecase struct {
	store   Store
	refresh func()
	audit   func(string)
}

func NewUsecase(store Store, refresh func(), audit func(string)) Usecase {
	return Usecase{store: store, refresh: refresh, audit: audit}
}
func (usecase Usecase) Execute(input Input) error {
	if err := usecase.store.DisableTag(input.Tag); err != nil {
		return err
	}
	usecase.refresh()
	usecase.audit(input.Tag)
	return nil
}
