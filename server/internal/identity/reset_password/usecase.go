package reset_password

import "errors"

type Mutation interface {
	AdvanceAuthVersion() error
	SavePassword(string) error
}
type Store interface {
	FindUnique(string) error
	Hash(string) (string, error)
	Transaction(func(Mutation) error) error
	PublishAuthCache() error
	RevokeSessions(string) error
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute(email, password string) error {
	if email == "" || password == "" {
		return errors.New("邮箱地址或密码为空！")
	}
	if err := u.store.FindUnique(email); err != nil {
		return err
	}
	hash, err := u.store.Hash(password)
	if err != nil {
		return err
	}
	if err = u.store.Transaction(func(tx Mutation) error {
		if err := tx.AdvanceAuthVersion(); err != nil {
			return err
		}
		return tx.SavePassword(hash)
	}); err != nil {
		return err
	}
	if err = u.store.PublishAuthCache(); err != nil {
		return err
	}
	return u.store.RevokeSessions("password_reset")
}
