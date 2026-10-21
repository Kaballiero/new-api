package webauthn

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	passkeysvc "github.com/QuantumNous/new-api/service/passkey"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	"net/http"
	"strconv"
	"time"
)

type Ceremony struct {
	ParentFlowToken string
	RawAssertion    []byte
	Metadata        func() (string, string)
	verification    *service.LoginVerification

	Request           *http.Request
	User              *model.User
	Authorization     *model.AuthFlowAuthorization
	IdentityReader    func() (domain.AuthIdentity, bool)
	Identity          domain.AuthIdentity
	FlowToken         string
	VerificationInput dto.PasskeyVerifyBeginRequest
	ParsedCreation    *protocol.ParsedCredentialCreationData
	ParsedAssertion   *protocol.ParsedCredentialAssertionData
	credential        *model.PasskeyCredential
	wa                *webauthnlib.WebAuthn
	data              *webauthnlib.SessionData
	security          *passkeysvc.FlowSecurity
	validated         *webauthnlib.Credential
	creation          *protocol.CredentialCreation
	assertion         *protocol.CredentialAssertion
	binding           service.VerificationBinding
	challenge         dto.PasskeyChallenge
	bundle            *dto.AuthBundle
	proof             *dto.SecurityProof
	loginAccount      *model.User
}

func (op *Ceremony) LoadCredential() error {
	var err error
	op.credential, err = model.GetPasskeyByUserID(op.User.Id)
	return err
}
func (op *Ceremony) CredentialMissing(err error) bool {
	return errors.Is(err, model.ErrPasskeyNotFound)
}
func (op *Ceremony) ClearCredential() { op.credential = nil }
func (op *Ceremony) Build() error {
	var err error
	op.wa, err = passkeysvc.BuildWebAuthn(op.Request)
	return err
}
func (op *Ceremony) ReadIdentity() bool {
	var ok bool
	op.Identity, ok = op.IdentityReader()
	return ok
}
func (op *Ceremony) BeginRegistration() error {
	user := passkeysvc.NewWebAuthnUser(op.User, op.credential)
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
	token, expires, err := passkeysvc.CreateSessionDataFlow(model.AuthFlowPurposePasskeyRegister, passkeysvc.FlowSecurity{AuthSessionIdentity: op.Identity, Scope: op.Authorization.Scope, ContextHash: op.Authorization.ContextHash, Authorization: op.Authorization}, op.data)
	op.challenge = dto.PasskeyChallenge{Options: op.creation, FlowToken: token, ExpiresAt: expires}
	return err
}
func (op *Ceremony) PopRegistrationFlow() error {
	var err error
	op.data, op.security, err = passkeysvc.PopSessionDataFlow(op.FlowToken, model.AuthFlowPurposePasskeyRegister, op.Identity)
	return err
}
func (op *Ceremony) VerificationRequired() bool {
	return op.data.UserVerification == protocol.VerificationRequired
}
func (op *Ceremony) ValidateEnrollmentAuthorization() error {
	return service.ValidateFlowAuthorization(op.Identity, service.VerificationOperation{Scope: service.VerificationScopePasskeyRegister}, op.security.Authorization)
}
func (op *Ceremony) CreateCredential() error {
	var err error
	op.validated, err = op.wa.CreateCredential(passkeysvc.NewWebAuthnUser(op.User, op.credential), *op.data, op.ParsedCreation)
	if err != nil {
		return err
	}
	op.credential = model.NewPasskeyCredentialFromWebAuthn(op.User.Id, op.validated)
	return nil
}
func (op *Ceremony) CredentialRecordValid() bool { return op.credential != nil }
func (op *Ceremony) RegisterCredential() error {
	return model.RegisterPasskeyForSession(op.Identity, op.credential)
}
func (op *Ceremony) AdvanceSession() error {
	var err error
	op.bundle, err = service.AdvanceCurrentSessionToUserVersion(op.Identity, "passkey_registered")
	return err
}
func (op *Ceremony) BeginDiscoverable() error {
	var err error
	op.assertion, op.data, err = op.wa.BeginDiscoverableLogin(webauthnlib.WithUserVerification(protocol.VerificationRequired))
	return err
}
func (op *Ceremony) CreateLoginFlow() error {
	token, expires, err := passkeysvc.CreateSessionDataFlow(model.AuthFlowPurposePasskeyLogin, passkeysvc.FlowSecurity{}, op.data)
	op.challenge = dto.PasskeyChallenge{Options: op.assertion, FlowToken: token, ExpiresAt: expires}
	return err
}
func (op *Ceremony) PopLoginFlow() error {
	var err error
	op.data, _, err = passkeysvc.PopSessionDataFlow(op.FlowToken, model.AuthFlowPurposePasskeyLogin, service.AuthIdentity{})
	return err
}
func (op *Ceremony) ValidateDiscoverable() error {
	handler := func(rawID, userHandle []byte) (webauthnlib.User, error) {
		credential, err := model.GetPasskeyByCredentialID(rawID)
		if err != nil {
			return nil, fmt.Errorf("未找到 Passkey 凭证: %w", err)
		}
		user := &model.User{Id: credential.UserID}
		if err := user.FillUserById(); err != nil {
			return nil, fmt.Errorf("用户信息获取失败: %w", err)
		}
		if user.Status != common.UserStatusEnabled {
			return nil, model.ErrUserSessionInactive
		}
		if len(userHandle) > 0 {
			userID, parseErr := strconv.Atoi(string(userHandle))
			if parseErr != nil {
				common.SysLog(fmt.Sprintf("PasskeyLogin: userHandle parse error for credential, length: %d", len(userHandle)))
			} else if userID != user.Id {
				return nil, errors.New("用户句柄与凭证不匹配")
			}
		}
		return passkeysvc.NewWebAuthnUser(user, credential), nil
	}
	waUser, credential, err := op.wa.ValidatePasskeyLogin(handler, *op.data, op.ParsedAssertion)
	if err != nil {
		return err
	}
	op.validated = credential
	wrapper, ok := waUser.(*passkeysvc.WebAuthnUser)
	if ok {
		op.loginAccount = wrapper.ModelUser()
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
	return model.UpdatePasskeyAssertionState(user.Id, op.validated, time.Now())
}
func (op *Ceremony) BindOperation() error {
	var err error
	op.binding, err = service.BindVerificationOperation(service.VerificationOperation{Scope: op.VerificationInput.Scope, Context: op.VerificationInput.Context})
	return err
}
func (op *Ceremony) RequireMethod() error {
	_, err := service.RequireVerificationMethod(op.Identity, op.VerificationInput.Scope, service.VerificationMethodPasskey)
	return err
}
func (op *Ceremony) BeginLogin() error {
	var err error
	op.assertion, op.data, err = op.wa.BeginLogin(passkeysvc.NewWebAuthnUser(op.User, op.credential), webauthnlib.WithUserVerification(protocol.VerificationRequired))
	return err
}
func (op *Ceremony) CreateVerificationFlow() error {
	token, expires, err := passkeysvc.CreateSessionDataFlow(model.AuthFlowPurposePasskeyStepUp, passkeysvc.FlowSecurity{AuthSessionIdentity: op.Identity, Scope: op.binding.Scope, ContextHash: op.binding.ContextHash}, op.data)
	op.challenge = dto.PasskeyChallenge{Options: op.assertion, FlowToken: token, ExpiresAt: expires}
	return err
}
func (op *Ceremony) PopVerificationFlow() error {
	var err error
	op.data, op.security, err = passkeysvc.PopSessionDataFlow(op.FlowToken, model.AuthFlowPurposePasskeyStepUp, op.Identity)
	return err
}
func (op *Ceremony) ValidateLogin() error {
	var err error
	op.validated, err = op.wa.ValidateLogin(passkeysvc.NewWebAuthnUser(op.User, op.credential), *op.data, op.ParsedAssertion)
	return err
}
func (op *Ceremony) IssueProof() error {
	var err error
	op.proof, err = service.CompleteSecurityVerification(op.Identity, service.VerificationBinding{Scope: op.security.Scope, ContextHash: op.security.ContextHash}, service.VerificationMethodPasskey)
	return err
}
func (op *Ceremony) Challenge() dto.PasskeyChallenge { return op.challenge }
func (op *Ceremony) Bundle() *dto.AuthBundle         { return op.bundle }
func (op *Ceremony) Proof() *dto.SecurityProof       { return op.proof }
func (op *Ceremony) LoginAccount() *model.User       { return op.loginAccount }

func (op *Ceremony) RequireLoginVerification() error {
	var err error
	op.verification, err = service.RequireLoginVerification(op.ParentFlowToken, service.VerificationMethodPasskey)
	if err != nil {
		return err
	}
	op.User = &model.User{Id: op.verification.State.UserID}
	return nil
}
func (op *Ceremony) CreateBoundLoginFlow() error {
	token, expires, err := passkeysvc.CreateSessionDataFlow(model.AuthFlowPurposeLoginPasskey, passkeysvc.FlowSecurity{AuthSessionIdentity: model.AuthSessionIdentity{UserID: op.User.Id, UserAuthVersion: op.verification.State.AuthVersion}, LoginFlowID: op.verification.Flow.Id, LoginExpiresAt: op.verification.Flow.ExpiresAt.Unix()}, op.data)
	op.challenge = dto.PasskeyChallenge{Options: op.assertion, FlowToken: token, ExpiresAt: expires}
	return err
}
func (op *Ceremony) ParseAssertion() error {
	var err error
	op.ParsedAssertion, err = protocol.ParseCredentialRequestResponseBytes(op.RawAssertion)
	return err
}
func (op *Ceremony) PopBoundLoginFlow() error {
	op.Identity = model.AuthSessionIdentity{UserID: op.verification.State.UserID, UserAuthVersion: op.verification.State.AuthVersion}
	var err error
	op.data, op.security, err = passkeysvc.PopSessionDataFlow(op.FlowToken, model.AuthFlowPurposeLoginPasskey, op.Identity)
	return err
}
func (op *Ceremony) BoundFlowValid() bool {
	return op.security.LoginFlowID == op.verification.Flow.Id && op.data.UserVerification == protocol.VerificationRequired
}
func (op *Ceremony) CompleteLogin() error {
	ip, ua := op.Metadata()
	var err error
	op.bundle, err = service.CompleteLoginVerification(op.ParentFlowToken, op.verification, service.VerificationMethodPasskey, ip, ua)
	return err
}
