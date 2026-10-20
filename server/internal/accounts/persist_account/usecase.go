package persist_account

type Mutation interface {
	HashPassword() error
	Prepare()
	LoadCurrent() (Security, error)
	RequestedSecurity() Security
	AdvanceAuthVersion() error
	Save() error
	Reload() error
}
type Store interface {
	PreviousAuthVersion() (int64, error)
	Transaction(func(Mutation) error) error
	UpdateCache() error
	AuthVersion() int64
	RevokeSessions(string) error
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute(password, edit bool) error {
	previous, err := u.store.PreviousAuthVersion()
	if err != nil {
		return err
	}
	if err = u.store.Transaction(func(tx Mutation) error { return u.WithTransaction(tx, password, edit) }); err != nil {
		return err
	}
	if err = u.store.UpdateCache(); err != nil {
		return err
	}
	if u.store.AuthVersion() > previous {
		return u.store.RevokeSessions("user_security_changed")
	}
	return nil
}
func (u Usecase) WithTransaction(tx Mutation, password, edit bool) error {
	if password {
		if err := tx.HashPassword(); err != nil {
			return err
		}
	}
	tx.Prepare()
	current, err := tx.LoadCurrent()
	if err != nil {
		return err
	}
	requested := tx.RequestedSecurity()
	changed := password && current.Password != requested.Password
	if edit {
		changed = changed || current.Group != requested.Group
	} else {
		changed = changed || requested.Role != 0 && current.Role != requested.Role || requested.Status != 0 && current.Status != requested.Status || requested.Group != "" && current.Group != requested.Group
	}
	if changed {
		if err := tx.AdvanceAuthVersion(); err != nil {
			return err
		}
	}
	if err := tx.Save(); err != nil {
		return err
	}
	return tx.Reload()
}
