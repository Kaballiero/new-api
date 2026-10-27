package accountsidentity

import (
	"github.com/QuantumNous/new-api/internal/accounts/update_profile"
	cacheaccounts "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/identity/change_self_password"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"

	emailadapter "github.com/QuantumNous/new-api/internal/adapter/identity/emailbinding"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
)

type PasswordCapabilities struct {
	ChangePassword func(identitydomain.AuthIdentity, *accountrecord.User, bool) error
	AdvanceSession func(identitydomain.AuthIdentity, string) (*identitydto.AuthBundle, error)
}

type PasswordChange struct {
	Capabilities PasswordCapabilities
	Accounts     accountstore.Store
	Cache        cacheaccounts.Store

	context   *gin.Context
	succeeded *bool
}

func NewPasswordChange(c *gin.Context, succeeded *bool, accounts accountstore.Store, cache cacheaccounts.Store, capabilities PasswordCapabilities) update_profile.PasswordChanger {
	return PasswordChange{context: c, succeeded: succeeded, Accounts: accounts, Cache: cache, Capabilities: capabilities}
}
func (change PasswordChange) Change(input update_profile.AccountInput, id int) (update_profile.PasswordResult, error) {
	identity, ok := middleware.GetSessionAuthIdentity(change.context)
	if !ok {
		return update_profile.PasswordResult{}, identitydomain.ErrAuthTokenInvalid
	}
	op := &selfPasswordOperation{Accounts: change.Accounts, Cache: change.Cache, change: change, identity: identity, input: input, id: id}
	outcome, err := change_self_password.NewUsecase(op).Execute()
	result := update_profile.PasswordResult{ProofRejected: outcome.ProofRejected, NotificationFailed: outcome.NotificationFailed}
	if err != nil || result.ProofRejected {
		return result, err
	}
	bundle := op.bundle
	result.Data = gin.H{"access_token": bundle.AccessToken, "token_type": bundle.TokenType, "access_expires_at": bundle.AccessExpiresAt, "session": bundle.Session, "has_password": true, "notification_warning": result.NotificationFailed}
	return result, nil
}

type selfPasswordOperation struct {
	Cache cacheaccounts.Store

	Accounts accountstore.Store

	change   PasswordChange
	identity identitydomain.AuthIdentity
	input    update_profile.AccountInput
	id       int
	current  *accountrecord.User
	clean    accountrecord.User
	bundle   *identitydto.AuthBundle
}

func (op *selfPasswordOperation) LoadCurrent() (bool, error) {
	var err error
	op.current, err = op.Accounts.GetUserById(op.identity.UserID, true)
	if err != nil {
		return false, err
	}
	return op.current.Password != "", nil
}
func (op *selfPasswordOperation) RequireProof(scope string) (bool, error) {
	return middleware.RequireSecurityProof(op.change.context, identitydomain.VerificationOperation{Scope: scope}) != nil, nil
}
func (op *selfPasswordOperation) ChangePassword(first bool) error {
	op.clean = accountrecord.User{Id: op.id, Username: op.input.Username, Password: op.input.Password, DisplayName: op.input.DisplayName, OriginalPassword: op.input.OriginalPassword}
	return op.change.Capabilities.ChangePassword(op.identity, &op.clean, first)
}
func (op *selfPasswordOperation) MarkSucceeded() { *op.change.succeeded = true }
func (op *selfPasswordOperation) NotifyPasswordUpdated() bool {
	return emailadapter.NotifyAccountSecurityChange(op.current.Email, "Password updated") != nil
}
func (op *selfPasswordOperation) PublishAuthCache() error {
	return op.Cache.PublishUserAuthCache(op.clean.Id)
}
func (op *selfPasswordOperation) AdvanceCurrentSession() error {
	var err error
	op.bundle, err = op.change.Capabilities.AdvanceSession(op.identity, "password_changed")
	return err
}
