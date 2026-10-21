package oauthadapter

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Callback struct {
	*Account
	Context                  *gin.Context
	Name                     string
	provider                 oauth.Provider
	state                    string
	pending, flow            *model.AuthFlow
	match                    model.AuthFlowMatch
	identity                 service.AuthIdentity
	payload, telegramPayload FlowPayload
	token                    *oauth.OAuthToken
	proof                    *dto.SecurityProof
	loaded                   bool
}

func (op *Callback) ProviderKnown() bool {
	op.provider = oauth.GetProvider(op.Name)
	return op.provider != nil
}
func (op *Callback) LoadPending() error {
	op.state = op.Context.Query("state")
	var err error
	op.pending, err = model.GetAuthFlow(op.state, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth, Provider: op.Name})
	if err == nil {
		op.loaded = true
		op.match = model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth, Provider: op.Name, Intent: op.pending.Intent}
	}
	return err
}
func (op *Callback) PendingIntent() string { return op.pending.Intent }
func (op *Callback) ReadBoundIdentity() bool {
	identity, ok := middleware.GetSessionAuthIdentity(op.Context)
	if !ok || identity.UserID != op.pending.UserId || identity.SessionID != op.pending.SessionId {
		return false
	}
	op.identity = identity
	op.match.UserId, op.match.SessionId = identity.UserID, identity.SessionID
	return true
}
func (op *Callback) ValidateBindingAuthorization() error {
	context, err := common.Marshal(service.AccountBindingContext{Provider: op.Name})
	if err != nil {
		return err
	}
	return service.ValidateFlowAuthorization(op.identity, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: context}, op.payload.Authorization)
}
func (op *Callback) IsTelegram() bool             { return op.Name == "telegram" }
func (op *Callback) TelegramConfiguration() error { return oauth.TelegramConfigurationError() }
func (op *Callback) DecodeTelegram() error {
	if err := common.UnmarshalJsonStr(op.pending.Payload, &op.telegramPayload); err != nil || op.telegramPayload.Telegram == nil {
		return model.ErrAuthFlowInvalid
	}
	return nil
}
func (op *Callback) ReadTelegramIdentity() bool {
	op.identity, _ = middleware.GetSessionAuthIdentity(op.Context)
	return op.telegramPayload.SessionIdentity != nil && *op.telegramPayload.SessionIdentity == op.identity
}
func (op *Callback) ValidateSession() error {
	_, _, err := service.ValidateLoginSession(op.identity)
	return err
}
func (op *Callback) CaptureTelegramContext() {
	op.Context.Set(oauth.TelegramOAuthFlowContextKey, op.telegramPayload.Telegram)
}
func (op *Callback) ProviderEnabled() bool       { return op.provider.IsEnabled() }
func (op *Callback) ProviderDisplayName() string { return op.provider.GetName() }
func (op *Callback) ProviderError() string       { return op.Context.Query("error") }
func (op *Callback) ProviderErrorDescription(code string) string {
	description := op.Context.Query("error_description")
	if description == "" {
		description = code
	}
	return description
}
func (op *Callback) ConsumeFlow() error {
	var err error
	op.flow, err = model.ConsumeAuthFlow(op.state, op.match)
	return err
}
func (op *Callback) ExchangeCode() error {
	code := op.Context.Query("code")
	var err error
	op.token, err = op.provider.ExchangeToken(op.Context.Request.Context(), code, op.Context)
	return err
}
func (op *Callback) ReadProviderUser() error {
	user, err := op.provider.GetUserInfo(op.Context.Request.Context(), op.token)
	if err == nil {
		op.Account = &Account{Provider: op.provider, OAuthUser: user, User: &model.User{}, Lang: i18n.GetLangFromContext(op.Context)}
	}
	return err
}
func (op *Callback) ConsumedIntent() string { return op.flow.Intent }
func (op *Callback) DecodePayload() error {
	return common.UnmarshalJsonStr(op.flow.Payload, &op.payload)
}
func (op *Callback) AffiliateCode() string { return op.payload.AffiliateCode }
func (op *Callback) AccountEnabled() bool  { return op.User.Status == common.UserStatusEnabled }
func (op *Callback) VerifyBoundAccount() error {
	op.identity, _ = middleware.GetSessionAuthIdentity(op.Context)
	var err error
	op.proof, err = service.FinishOAuthVerification(op.identity, op.Name, op.OAuthUser.ProviderUserID, op.payload.Verification)
	return err
}
func (op *Callback) ReadBindingIdentity() bool {
	var ok bool
	op.identity, ok = middleware.GetSessionAuthIdentity(op.Context)
	return ok
}
func (op *Callback) DecodeBindingPayload() error {
	if err := common.UnmarshalJsonStr(op.pending.Payload, &op.payload); err != nil {
		return model.ErrAuthFlowInvalid
	}
	return nil
}
func (op *Callback) ProviderUserTaken() bool {
	return op.provider.IsUserIDTaken(op.OAuthUser.ProviderUserID)
}
func (op *Callback) LegacyUserTaken() bool {
	legacy, ok := op.OAuthUser.Extra["legacy_id"].(string)
	return ok && legacy != "" && op.provider.IsUserIDTaken(legacy)
}
func (op *Callback) CommitBinding() error {
	_, err := model.ConsumeAuthFlowWithAction(op.state, op.match, func(tx *gorm.DB, _ *model.AuthFlow) error {
		if op.Name == "telegram" {
			return model.BindTelegramForSessionWithTx(tx, op.identity, op.OAuthUser.ProviderUserID)
		}
		if provider, ok := op.provider.(*oauth.GenericOAuthProvider); ok {
			return model.UpdateUserOAuthBindingForSessionWithTx(tx, op.identity, provider.GetProviderId(), op.OAuthUser.ProviderUserID)
		}
		return model.UpdateUserBindColumnForSessionWithTx(tx, op.identity, op.provider.ProviderUserIDColumn(), op.OAuthUser.ProviderUserID)
	})
	return err
}
func (op *Callback) LoadBoundAccount() error {
	var err error
	op.Account.User, err = model.GetUserById(op.identity.UserID, false)
	return err
}
func (op *Callback) NotifyBoundAccount() bool {
	return service.NotifyAccountSecurityChange(op.User.Email, "Login account linked: "+op.provider.GetName()) != nil
}
func (op *Callback) BindingAudit() (bool, int) {
	if !op.loaded || op.pending.Intent != model.AuthFlowIntentBind {
		return false, 0
	}
	return true, op.pending.UserId
}
func (op *Callback) Proof() *dto.SecurityProof      { return op.proof }
func (op *Callback) ProofUserID() int               { return op.identity.UserID }
func (op *Callback) AuthenticatedUser() *model.User { return op.User }
