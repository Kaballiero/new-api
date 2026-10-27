package oauthadapter

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_flow"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-gonic/gin"
	"time"

	"github.com/QuantumNous/new-api/internal/adapter/identity/verification"
	"github.com/QuantumNous/new-api/internal/app/identitycore"
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type FlowPayload struct {
	AffiliateCode   string                              `json:"affiliate_code,omitempty"`
	Verification    *verification.OAuthVerificationFlow `json:"verification,omitempty"`
	Telegram        *oauth.TelegramOAuthFlow            `json:"telegram,omitempty"`
	SessionIdentity *domain.AuthIdentity                `json:"session_identity,omitempty"`
	Authorization   *domain.FlowAuthorization           `json:"authorization,omitempty"`
}
type Start struct {
	state                    string
	expiresAt                int64
	Context                  *gin.Context
	Input                    *start_oauth_flow.Request
	payload                  FlowPayload
	identity                 domain.AuthIdentity
	userID                   int
	sessionID                string
	AuditRequired, Succeeded bool
	Data                     map[string]any
}

func (op *Start) ProviderKnown() bool { return oauth.GetProvider(op.Input.Provider) != nil }
func (op *Start) InitializePayload()  { op.payload = FlowPayload{AffiliateCode: op.Input.Aff} }
func (op *Start) NewTelegramFlow() error {
	flow, err := oauth.NewTelegramOAuthFlow()
	op.payload.Telegram = flow
	return err
}
func (op *Start) ReadIdentity() bool {
	var ok bool
	op.identity, ok = middleware.GetSessionAuthIdentity(op.Context)
	if ok {
		op.userID, op.sessionID = op.identity.UserID, op.identity.SessionID
	}
	return ok
}
func (op *Start) BeginBindingAudit() { op.AuditRequired = true }
func (op *Start) RequireBindingProof() (bool, error) {
	context, err := common.Marshal(domain.AccountBindingContext{Provider: op.Input.Provider})
	if err != nil {
		return false, err
	}
	op.payload.Authorization = middleware.RequireSecurityProof(op.Context, domain.VerificationOperation{Scope: domain.VerificationScopeAccountBind, Context: context})
	return op.payload.Authorization != nil, nil
}
func (op *Start) HasTelegram() bool { return op.payload.Telegram != nil }
func (op *Start) ValidateSession() error {
	_, _, err := identitycore.ValidateLoginSession(op.identity)
	return err
}
func (op *Start) CaptureSession() { identity := op.identity; op.payload.SessionIdentity = &identity }
func (op *Start) StartVerification() error {
	verification, err := identitycore.StartOAuthVerification(op.identity, domain.VerificationOperation{Scope: op.Input.Scope, Context: op.Input.Context}, op.Input.Provider)
	op.payload.Verification = verification
	return err
}
func (op *Start) CreateFlow() error {
	payload, err := common.Marshal(op.payload)
	if err != nil {
		return err
	}
	expires := time.Now().Add(10 * time.Minute)
	state, _, err := model.CreateAuthFlow(model.AuthFlowCreate{Purpose: model.AuthFlowPurposeOAuth, Provider: op.Input.Provider, Intent: op.Input.Intent, UserId: op.userID, SessionId: op.sessionID, Payload: string(payload), ExpiresAt: expires})
	if err != nil {
		return err
	}
	op.state, op.expiresAt = state, expires.Unix()
	return nil
}
func (op *Start) MarkSucceeded() {
	op.Succeeded = op.Input.Intent == model.AuthFlowIntentBind
	op.Data = map[string]any{"flow_token": op.state, "expires_at": op.expiresAt}
	if op.payload.Telegram != nil {
		op.Data["authorization_url"] = op.payload.Telegram.AuthorizationURL(op.state)
	}
}
func (op *Start) BindingAudit() (required bool, userID int, succeeded bool) {
	return op.AuditRequired, op.userID, op.Succeeded
}
func (op *Start) Response() map[string]any { return op.Data }
