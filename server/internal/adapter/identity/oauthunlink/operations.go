package oauthadapter

import (
	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

type UnlinkMutation struct {
	SQL identitystore.Store

	Identity   domain.AuthIdentity
	ProviderID int
}

func (UnlinkMutation) EnabledMethods() domain.LoginMethods {
	enabled := domain.LoginMethods{Password: common.PasswordLoginEnabled, Passkey: system_setting.GetPasskeySettings().Enabled, WeChat: common.WeChatAuthEnabled}
	for _, provider := range oauth.GetAllProviders() {
		if !provider.IsEnabled() {
			continue
		}
		if custom, ok := provider.(*oauth.GenericOAuthProvider); ok {
			enabled.CustomProviderIDs = append(enabled.CustomProviderIDs, custom.GetProviderId())
		} else {
			enabled.OAuthColumns = append(enabled.OAuthColumns, provider.ProviderUserIDColumn())
		}
	}
	return enabled
}
func (op UnlinkMutation) Unlink(enabled domain.LoginMethods) error {
	return op.SQL.UnbindUserOAuthForSession(op.Identity, op.ProviderID, enabled)
}

type UnlinkConfirmation struct {
	Accounts accountstore.Store

	Identity           domain.AuthIdentity
	ProviderID         int
	UnlinkFunc         func(domain.AuthIdentity, int) error
	Notify             func(string, string) error
	user               *accountrecord.User
	succeeded, warning bool
}

func (op *UnlinkConfirmation) Unlink() error  { return op.UnlinkFunc(op.Identity, op.ProviderID) }
func (op *UnlinkConfirmation) MarkSucceeded() { op.succeeded = true }
func (op *UnlinkConfirmation) LoadAccount() error {
	var err error
	op.user, err = op.Accounts.GetUserById(op.Identity.UserID, false)
	return err
}
func (op *UnlinkConfirmation) NotifyAccount() bool {
	return op.Notify(op.user.Email, "Login account unlinked") != nil
}
func (op *UnlinkConfirmation) SetNotificationWarning(warning bool) { op.warning = warning }
func (op *UnlinkConfirmation) Succeeded() bool                     { return op.succeeded }
func (op *UnlinkConfirmation) NotificationWarning() bool           { return op.warning }
