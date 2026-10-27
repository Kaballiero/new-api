package accountsidentity

import (
	"github.com/QuantumNous/new-api/internal/accounts/update_profile"
	"github.com/QuantumNous/new-api/internal/identity/change_self_password"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"

	emailadapter "github.com/QuantumNous/new-api/internal/adapter/identity/emailbinding"
	identitycore "github.com/QuantumNous/new-api/internal/app/identitycore"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
)

type PasswordChange struct {
	context   *gin.Context
	succeeded *bool
}

func NewPasswordChange(c *gin.Context, succeeded *bool) update_profile.PasswordChanger {
	return PasswordChange{context: c, succeeded: succeeded}
}
func (change PasswordChange) Change(input update_profile.AccountInput, id int) (update_profile.PasswordResult, error) {
	identity, ok := middleware.GetSessionAuthIdentity(change.context)
	if !ok {
		return update_profile.PasswordResult{}, identitydomain.ErrAuthTokenInvalid
	}
	op := &selfPasswordOperation{change: change, identity: identity, input: input, id: id}
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
	change   PasswordChange
	identity identitydomain.AuthIdentity
	input    update_profile.AccountInput
	id       int
	current  *model.User
	clean    model.User
	bundle   *identitydto.AuthBundle
}

func (op *selfPasswordOperation) LoadCurrent() (bool, error) {
	var err error
	op.current, err = model.GetUserById(op.identity.UserID, true)
	if err != nil {
		return false, err
	}
	return op.current.Password != "", nil
}
func (op *selfPasswordOperation) RequireProof(scope string) (bool, error) {
	return middleware.RequireSecurityProof(op.change.context, identitydomain.VerificationOperation{Scope: scope}) != nil, nil
}
func (op *selfPasswordOperation) ChangePassword(first bool) error {
	op.clean = model.User{Id: op.id, Username: op.input.Username, Password: op.input.Password, DisplayName: op.input.DisplayName, OriginalPassword: op.input.OriginalPassword}
	return model.ChangeUserPassword(op.identity, &op.clean, first)
}
func (op *selfPasswordOperation) MarkSucceeded() { *op.change.succeeded = true }
func (op *selfPasswordOperation) NotifyPasswordUpdated() bool {
	return emailadapter.NotifyAccountSecurityChange(op.current.Email, "Password updated") != nil
}
func (op *selfPasswordOperation) PublishAuthCache() error {
	return model.PublishUserAuthCache(op.clean.Id)
}
func (op *selfPasswordOperation) AdvanceCurrentSession() error {
	var err error
	op.bundle, err = identitycore.AdvanceCurrentSessionToUserVersion(op.identity, "password_changed")
	return err
}
