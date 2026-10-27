package emailbinding

import (
	cacheaccounts "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Confirmation struct {
	Cache cacheaccounts.Store

	Identity                domain.AuthIdentity
	Token, NewCode, OldCode string
	Finish                  func(domain.AuthIdentity, string, string, string) (*identitystore.EmailBindingState, error)
	Notify                  func(string, string) error
	state                   *identitystore.EmailBindingState
	succeeded, warning      bool
}

func (op *Confirmation) FinishBinding() error {
	var err error
	op.state, err = op.Finish(op.Identity, op.Token, op.NewCode, op.OldCode)
	return err
}
func (op *Confirmation) MarkSucceeded() { op.succeeded = true }
func (op *Confirmation) NotifyPrevious() bool {
	return op.Notify(op.state.CurrentEmail, "Email address changed") != nil
}
func (op *Confirmation) NotifyConfirmed() error {
	return op.Notify(op.state.Email, "Email address confirmed")
}
func (op *Confirmation) SetNotificationWarning(warning bool) { op.warning = warning }
func (op *Confirmation) PublishCache() error {
	return op.Cache.PublishUserAuthCache(op.Identity.UserID)
}
func (op *Confirmation) Succeeded() bool           { return op.succeeded }
func (op *Confirmation) NotificationWarning() bool { return op.warning }
