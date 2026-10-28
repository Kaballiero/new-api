package bind_email

type Mutation interface {
	EnsureAvailable(string) error
	Assign(string)
	Save() error
}
type Store interface {
	Normalize(string) string
	Transaction(string, func(Mutation) error) error
	UpdateCache() error
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute(email string) error {
	email = u.store.Normalize(email)
	if err := u.store.Transaction(email, func(tx Mutation) error {
		if err := tx.EnsureAvailable(email); err != nil {
			return err
		}
		tx.Assign(email)
		return tx.Save()
	}); err != nil {
		return err
	}
	return u.store.UpdateCache()
}
