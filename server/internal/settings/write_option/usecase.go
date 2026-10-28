package write_option

type Store interface{ SaveLegacy(string, string) }
type Routing struct {
	IsBulk    func(string) bool
	IsPricing func(string) bool
}
type Writers struct {
	Bulk     func(map[string]string) error
	Pricing  func(map[string]string) error
	Publish  func(string, string) error
	Validate func(string, string) error
}
type Usecase struct {
	store   Store
	routing Routing
	writers Writers
}

func NewUsecase(store Store, routing Routing, writers Writers) Usecase {
	return Usecase{store, routing, writers}
}
func (u Usecase) Execute(key, value string) error {
	if u.routing.IsBulk(key) {
		return u.writers.Bulk(map[string]string{key: value})
	}
	if u.routing.IsPricing(key) {
		return u.writers.Pricing(map[string]string{key: value})
	}
	if err := u.writers.Validate(key, value); err != nil {
		return err
	}
	u.store.SaveLegacy(key, value)
	return u.writers.Publish(key, value)
}
