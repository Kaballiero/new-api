package webauthn

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"github.com/QuantumNous/new-api/internal/adapter/identity/login"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

type Ceremony struct {
	ValidateAuthorization   func(domain.AuthIdentity, domain.VerificationOperation, *domain.FlowAuthorization) error
	AdvanceSessionVersion   func(domain.AuthIdentity, string) (*dto.AuthBundle, error)
	BindVerification        func(domain.VerificationOperation) (domain.VerificationBinding, error)
	RequireVerification     func(domain.AuthIdentity, string, string) error
	CompleteVerification    func(domain.AuthIdentity, domain.VerificationBinding, string) (*dto.SecurityProof, error)
	LoadLoginVerification   func(string, string) (*login.LoginVerification, error)
	FinishLoginVerification func(string, *login.LoginVerification, string, string, string) (*dto.AuthBundle, error)
	Accounts                accountstore.Store

	SQL identitystore.Store

	CreateSessionDataFlow func(string, domain.PasskeyFlowSecurity, *webauthnlib.SessionData) (string, int64, error)
	PopSessionDataFlow    func(string, string, domain.AuthIdentity) (*webauthnlib.SessionData, *domain.PasskeyFlowSecurity, error)
	FinishLogin           func(*gin.Context, *accountrecord.User, int64)

	ParentFlowToken string
	RawAssertion    []byte
	Metadata        func() (string, string)
	verification    *login.LoginVerification

	Request           *http.Request
	User              *accountrecord.User
	Authorization     *domain.FlowAuthorization
	IdentityReader    func() (domain.AuthIdentity, bool)
	Identity          domain.AuthIdentity
	FlowToken         string
	VerificationInput dto.PasskeyVerifyBeginRequest
	ParsedCreation    *protocol.ParsedCredentialCreationData
	ParsedAssertion   *protocol.ParsedCredentialAssertionData
	credential        *identityrecord.PasskeyCredential
	wa                *webauthnlib.WebAuthn
	data              *webauthnlib.SessionData
	security          *domain.PasskeyFlowSecurity
	validated         *webauthnlib.Credential
	creation          *protocol.CredentialCreation
	assertion         *protocol.CredentialAssertion
	binding           domain.VerificationBinding
	challenge         dto.PasskeyChallenge
	bundle            *dto.AuthBundle
	proof             *dto.SecurityProof
	loginAccount      *accountrecord.User
}

func (op *Ceremony) LoadCredential() error {
	var err error
	op.credential, err = op.SQL.GetPasskeyByUserID(op.User.Id)
	return err
}
func (op *Ceremony) CredentialMissing(err error) bool {
	return errors.Is(err, domain.ErrPasskeyNotFound)
}
func (op *Ceremony) ClearCredential() { op.credential = nil }
func (op *Ceremony) Build() error {
	var err error
	op.wa, err = buildWebAuthn(op.Request)
	return err
}
func (op *Ceremony) ReadIdentity() bool {
	var ok bool
	op.Identity, ok = op.IdentityReader()
	return ok
}
func (op *Ceremony) BeginRegistration() error {
	user := newWebAuthnUser(op.User, op.credential)
	selection := op.wa.Config.AuthenticatorSelection
	selection.UserVerification = protocol.VerificationRequired
	options := []webauthnlib.RegistrationOption{webauthnlib.WithAuthenticatorSelection(selection)}
	if op.credential != nil {
		descriptor := op.credential.ToWebAuthnCredential().Descriptor()
		options = append(options, webauthnlib.WithExclusions([]protocol.CredentialDescriptor{descriptor}))
	}
	var err error
	op.creation, op.data, err = op.wa.BeginRegistration(user, options...)
	return err
}
func (op *Ceremony) CreateRegistrationFlow() error {
	token, expires, err := op.CreateSessionDataFlow(domain.AuthFlowPurposePasskeyRegister, domain.PasskeyFlowSecurity{AuthSessionIdentity: op.Identity, Scope: op.Authorization.Scope, ContextHash: op.Authorization.ContextHash, Authorization: op.Authorization}, op.data)
	op.challenge = dto.PasskeyChallenge{Options: op.creation, FlowToken: token, ExpiresAt: expires}
	return err
}
func (op *Ceremony) PopRegistrationFlow() error {
	var err error
	op.data, op.security, err = op.PopSessionDataFlow(op.FlowToken, domain.AuthFlowPurposePasskeyRegister, op.Identity)
	return err
}
func (op *Ceremony) VerificationRequired() bool {
	return op.data.UserVerification == protocol.VerificationRequired
}
func (op *Ceremony) ValidateEnrollmentAuthorization() error {
	return op.ValidateAuthorization(op.Identity, domain.VerificationOperation{Scope: domain.VerificationScopePasskeyRegister}, op.security.Authorization)
}
func (op *Ceremony) CreateCredential() error {
	var err error
	op.validated, err = op.wa.CreateCredential(newWebAuthnUser(op.User, op.credential), *op.data, op.ParsedCreation)
	if err != nil {
		return err
	}
	op.credential = identityrecord.NewPasskeyCredentialFromWebAuthn(op.User.Id, op.validated)
	return nil
}
func (op *Ceremony) CredentialRecordValid() bool { return op.credential != nil }
func (op *Ceremony) RegisterCredential() error {
	return op.SQL.RegisterPasskeyForSession(op.Identity, op.credential)
}
func (op *Ceremony) AdvanceSession() error {
	var err error
	op.bundle, err = op.AdvanceSessionVersion(op.Identity, "passkey_registered")
	return err
}
func (op *Ceremony) BeginDiscoverable() error {
	var err error
	op.assertion, op.data, err = op.wa.BeginDiscoverableLogin(webauthnlib.WithUserVerification(protocol.VerificationRequired))
	return err
}
func (op *Ceremony) CreateLoginFlow() error {
	token, expires, err := op.CreateSessionDataFlow(domain.AuthFlowPurposePasskeyLogin, domain.PasskeyFlowSecurity{}, op.data)
	op.challenge = dto.PasskeyChallenge{Options: op.assertion, FlowToken: token, ExpiresAt: expires}
	return err
}
func (op *Ceremony) PopLoginFlow() error {
	var err error
	op.data, _, err = op.PopSessionDataFlow(op.FlowToken, domain.AuthFlowPurposePasskeyLogin, domain.AuthIdentity{})
	return err
}
func (op *Ceremony) ValidateDiscoverable() error {
	handler := func(rawID, userHandle []byte) (webauthnlib.User, error) {
		credential, err := op.SQL.GetPasskeyByCredentialID(rawID)
		if err != nil {
			return nil, fmt.Errorf("passkey credential not found: %w", err)
		}
		user := &accountrecord.User{Id: credential.UserID}
		if err := op.Accounts.FillUserById(user); err != nil {
			return nil, fmt.Errorf("failed to load user: %w", err)
		}
		if user.Status != common.UserStatusEnabled {
			return nil, domain.ErrSessionInactive
		}
		if len(userHandle) > 0 {
			userID, parseErr := strconv.Atoi(string(userHandle))
			if parseErr != nil {
				common.SysLog(fmt.Sprintf("PasskeyLogin: userHandle parse error for credential, length: %d", len(userHandle)))
			} else if userID != user.Id {
				return nil, errors.New("user handle does not match the credential")
			}
		}
		return newWebAuthnUser(user, credential), nil
	}
	waUser, credential, err := op.wa.ValidatePasskeyLogin(handler, *op.data, op.ParsedAssertion)
	if err != nil {
		return err
	}
	op.validated = credential
	wrapper, ok := waUser.(*webAuthnUser)
	if ok {
		op.loginAccount = wrapper.user
	}
	return nil
}
func (op *Ceremony) LoginAccountValid() bool { return op.loginAccount != nil }
func (op *Ceremony) LoginAccountEnabled() bool {
	return op.loginAccount.Status == common.UserStatusEnabled
}
func (op *Ceremony) UpdateAssertion() error {
	user := op.User
	if op.loginAccount != nil {
		user = op.loginAccount
	}
	return op.SQL.UpdatePasskeyAssertionState(user.Id, op.validated, time.Now())
}
func (op *Ceremony) BindOperation() error {
	var err error
	op.binding, err = op.BindVerification(domain.VerificationOperation{Scope: op.VerificationInput.Scope, Context: op.VerificationInput.Context})
	return err
}
func (op *Ceremony) RequireMethod() error {
	return op.RequireVerification(op.Identity, op.VerificationInput.Scope, domain.VerificationMethodPasskey)
}
func (op *Ceremony) BeginLogin() error {
	var err error
	op.assertion, op.data, err = op.wa.BeginLogin(newWebAuthnUser(op.User, op.credential), webauthnlib.WithUserVerification(protocol.VerificationRequired))
	return err
}
func (op *Ceremony) CreateVerificationFlow() error {
	token, expires, err := op.CreateSessionDataFlow(domain.AuthFlowPurposePasskeyStepUp, domain.PasskeyFlowSecurity{AuthSessionIdentity: op.Identity, Scope: op.binding.Scope, ContextHash: op.binding.ContextHash}, op.data)
	op.challenge = dto.PasskeyChallenge{Options: op.assertion, FlowToken: token, ExpiresAt: expires}
	return err
}
func (op *Ceremony) PopVerificationFlow() error {
	var err error
	op.data, op.security, err = op.PopSessionDataFlow(op.FlowToken, domain.AuthFlowPurposePasskeyStepUp, op.Identity)
	return err
}
func (op *Ceremony) ValidateLogin() error {
	var err error
	op.validated, err = op.wa.ValidateLogin(newWebAuthnUser(op.User, op.credential), *op.data, op.ParsedAssertion)
	return err
}
func (op *Ceremony) IssueProof() error {
	var err error
	op.proof, err = op.CompleteVerification(op.Identity, domain.VerificationBinding{Scope: op.security.Scope, ContextHash: op.security.ContextHash}, domain.VerificationMethodPasskey)
	return err
}
func (op *Ceremony) Challenge() dto.PasskeyChallenge { return op.challenge }
func (op *Ceremony) Bundle() *dto.AuthBundle         { return op.bundle }
func (op *Ceremony) Proof() *dto.SecurityProof       { return op.proof }
func (op *Ceremony) CompletePasskeyLogin(c *gin.Context) {
	op.FinishLogin(c, op.loginAccount, op.loginAccount.AuthVersion)
}

func (op *Ceremony) RequireLoginVerification() error {
	var err error
	op.verification, err = op.LoadLoginVerification(op.ParentFlowToken, domain.VerificationMethodPasskey)
	if err != nil {
		return err
	}
	op.User = &accountrecord.User{Id: op.verification.State.UserID}
	return nil
}
func (op *Ceremony) CreateBoundLoginFlow() error {
	token, expires, err := op.CreateSessionDataFlow(domain.AuthFlowPurposeLoginPasskey, domain.PasskeyFlowSecurity{AuthSessionIdentity: domain.AuthIdentity{UserID: op.User.Id, UserAuthVersion: op.verification.State.AuthVersion}, LoginFlowID: op.verification.Flow.Id, LoginExpiresAt: op.verification.Flow.ExpiresAt.Unix()}, op.data)
	op.challenge = dto.PasskeyChallenge{Options: op.assertion, FlowToken: token, ExpiresAt: expires}
	return err
}
func (op *Ceremony) ParseAssertion() error {
	var err error
	op.ParsedAssertion, err = protocol.ParseCredentialRequestResponseBytes(op.RawAssertion)
	return err
}
func (op *Ceremony) PopBoundLoginFlow() error {
	op.Identity = domain.AuthIdentity{UserID: op.verification.State.UserID, UserAuthVersion: op.verification.State.AuthVersion}
	var err error
	op.data, op.security, err = op.PopSessionDataFlow(op.FlowToken, domain.AuthFlowPurposeLoginPasskey, op.Identity)
	return err
}
func (op *Ceremony) BoundFlowValid() bool {
	return op.security.LoginFlowID == op.verification.Flow.Id && op.data.UserVerification == protocol.VerificationRequired
}
func (op *Ceremony) CompleteLogin() error {
	ip, ua := op.Metadata()
	var err error
	op.bundle, err = op.FinishLoginVerification(op.ParentFlowToken, op.verification, domain.VerificationMethodPasskey, ip, ua)
	return err
}
