package clear_binding

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Mutation interface {
	ClearColumn(string) error
	TelegramProvider() string
	ReleaseTelegram() error
}
type Store interface {
	UserID() int
	Transaction(func(Mutation) error) error
	Reload() error
	UpdateCache() error
}
type BindingChange struct{ store Store }

func NewBindingChange(store Store) BindingChange { return BindingChange{store: store} }
func (u BindingChange) Execute(bindingType string) error {
	if u.store.UserID() == 0 {
		return errors.New("user id is empty")
	}
	columns := map[string]string{"email": "email", "github": "github_id", "discord": "discord_id", "oidc": "oidc_id", "wechat": "wechat_id", "telegram": "telegram_id", "linuxdo": "linux_do_id"}
	column, ok := columns[bindingType]
	if !ok {
		return errors.New("invalid binding type")
	}
	if err := u.store.Transaction(func(tx Mutation) error {
		if err := tx.ClearColumn(column); err != nil {
			return err
		}
		if bindingType == tx.TelegramProvider() {
			return tx.ReleaseTelegram()
		}
		return nil
	}); err != nil {
		return err
	}
	if err := u.store.Reload(); err != nil {
		return err
	}
	return u.store.UpdateCache()
}

// AdminOperation keeps the loaded account through its mutation and reload, so
// auditing observes the same post-mutation values as the existing endpoint.
type AdminOperation interface {
	LoadTarget(id int) (role int, err error)
	ActorRole() int
	RootRole() int
	Clear(bindingType string) error
	AuditCurrent(bindingType string)
}
type Usecase struct{ operation AdminOperation }

func NewUsecase(operation AdminOperation) Usecase { return Usecase{operation: operation} }

// Execute clears one binding of an account; load and clear failures are
// returned unclassified.
func (u Usecase) Execute(input Input) error {
	role, err := u.operation.LoadTarget(input.ID)
	if err != nil {
		return err
	}
	if !domain.CanManageTargetRole(u.operation.ActorRole(), role, u.operation.RootRole()) {
		return domain.ErrBindingForbidden
	}
	if err := u.operation.Clear(input.BindingType); err != nil {
		return err
	}
	u.operation.AuditCurrent(input.BindingType)
	return nil
}
