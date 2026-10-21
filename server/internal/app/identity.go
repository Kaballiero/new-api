package app

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	accountsidentity "github.com/QuantumNous/new-api/internal/adapter/identity/accounts"
	emailadapter "github.com/QuantumNous/new-api/internal/adapter/identity/emailbinding"
	"github.com/QuantumNous/new-api/internal/adapter/identity/factors"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	oauthadapter "github.com/QuantumNous/new-api/internal/adapter/identity/oauth"
	oauthunlink "github.com/QuantumNous/new-api/internal/adapter/identity/oauthunlink"
	"github.com/QuantumNous/new-api/internal/adapter/identity/passwordlogin"
	"github.com/QuantumNous/new-api/internal/adapter/identity/recovery"
	"github.com/QuantumNous/new-api/internal/adapter/identity/registration"
	sessionadapter "github.com/QuantumNous/new-api/internal/adapter/identity/session"
	webauthnadapter "github.com/QuantumNous/new-api/internal/adapter/identity/webauthn"
	wechatadapter "github.com/QuantumNous/new-api/internal/adapter/identity/wechat"
	"github.com/QuantumNous/new-api/internal/identity/admin_disable_twofa"
	"github.com/QuantumNous/new-api/internal/identity/admin_reset_passkey"
	"github.com/QuantumNous/new-api/internal/identity/admin_unlink_oauth_account"
	"github.com/QuantumNous/new-api/internal/identity/begin_login_passkey"
	"github.com/QuantumNous/new-api/internal/identity/begin_passkey_login"
	"github.com/QuantumNous/new-api/internal/identity/begin_passkey_registration"
	"github.com/QuantumNous/new-api/internal/identity/begin_passkey_verification"
	"github.com/QuantumNous/new-api/internal/identity/bind_wechat"
	"github.com/QuantumNous/new-api/internal/identity/complete_oauth"
	"github.com/QuantumNous/new-api/internal/identity/confirm_email_binding"
	selfdelete "github.com/QuantumNous/new-api/internal/identity/delete_account"
	"github.com/QuantumNous/new-api/internal/identity/delete_passkey"
	"github.com/QuantumNous/new-api/internal/identity/disable_twofa"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/finish_login_passkey"
	"github.com/QuantumNous/new-api/internal/identity/finish_passkey_login"
	"github.com/QuantumNous/new-api/internal/identity/finish_passkey_registration"
	"github.com/QuantumNous/new-api/internal/identity/finish_passkey_verification"
	"github.com/QuantumNous/new-api/internal/identity/finish_twofa_setup"
	"github.com/QuantumNous/new-api/internal/identity/get_oauth_bindings"
	"github.com/QuantumNous/new-api/internal/identity/get_passkey_status"
	"github.com/QuantumNous/new-api/internal/identity/get_password_encryption_key"
	"github.com/QuantumNous/new-api/internal/identity/get_twofa_stats"
	"github.com/QuantumNous/new-api/internal/identity/get_twofa_status"
	"github.com/QuantumNous/new-api/internal/identity/get_verification_requirements"
	"github.com/QuantumNous/new-api/internal/identity/list_sessions"
	"github.com/QuantumNous/new-api/internal/identity/login_wechat"
	"github.com/QuantumNous/new-api/internal/identity/logout"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
	"github.com/QuantumNous/new-api/internal/identity/recover_password"
	"github.com/QuantumNous/new-api/internal/identity/refresh_session"
	"github.com/QuantumNous/new-api/internal/identity/regenerate_backup_codes"
	"github.com/QuantumNous/new-api/internal/identity/register_password"
	"github.com/QuantumNous/new-api/internal/identity/request_password_reset"
	"github.com/QuantumNous/new-api/internal/identity/resend_email_binding"
	"github.com/QuantumNous/new-api/internal/identity/revoke_other_sessions"
	"github.com/QuantumNous/new-api/internal/identity/revoke_session"
	"github.com/QuantumNous/new-api/internal/identity/send_verification_email"
	"github.com/QuantumNous/new-api/internal/identity/start_email_binding"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_flow"
	"github.com/QuantumNous/new-api/internal/identity/start_twofa_setup"
	identitytransport "github.com/QuantumNous/new-api/internal/identity/transport"
	"github.com/QuantumNous/new-api/internal/identity/unlink_oauth_account"
	"github.com/QuantumNous/new-api/internal/identity/verify_login_code"
	"github.com/QuantumNous/new-api/internal/identity/verify_security_input"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"gorm.io/gorm"
)

type IdentityEffects struct {
	LoginAudit            func(*model.User, *gin.Context)
	CompleteOAuthLogin    func(*model.User, *gin.Context)
	ManageAudit           func(*gin.Context, int, string, map[string]any)
	CompletePasskeyLogin  func(*gin.Context, *model.User, int64)
	CompleteVerifiedLogin func(*gin.Context, *identitydto.AuthBundle, string)
	SecurityAudit         func(*gin.Context, int, string, map[string]any)
	BuildProfile          func(*model.User) map[string]any
	CompletePasswordLogin func(*gin.Context, password_login.Account)
}
type IdentityHTTP struct {
	Errors                    identitytransport.Writer
	GetOAuthBindings          *get_oauth_bindings.HTTP
	UnlinkOAuthAccount        *unlink_oauth_account.HTTP
	AdminUnlinkOAuthAccount   *admin_unlink_oauth_account.HTTP
	LoginWeChat               *login_wechat.HTTP
	BindWeChat                *bind_wechat.HTTP
	PasswordEncryptionKey     *get_password_encryption_key.HTTP
	PrimaryLogin              identityhttp.PrimaryLogin
	CompleteOAuth             *complete_oauth.HTTP
	StartOAuthFlow            *start_oauth_flow.HTTP
	DeleteAccount             *selfdelete.HTTP
	StartEmailBinding         *start_email_binding.HTTP
	ResendEmailBinding        *resend_email_binding.HTTP
	ConfirmEmailBinding       *confirm_email_binding.HTTP
	BeginLoginPasskey         *begin_login_passkey.HTTP
	FinishLoginPasskey        *finish_login_passkey.HTTP
	GetTwoFAStatus            *get_twofa_status.HTTP
	GetTwoFAStats             *get_twofa_stats.HTTP
	GetPasskeyStatus          *get_passkey_status.HTTP
	DeletePasskey             *delete_passkey.HTTP
	AdminDisableTwoFA         *admin_disable_twofa.HTTP
	AdminResetPasskey         *admin_reset_passkey.HTTP
	BeginPasskeyRegistration  *begin_passkey_registration.HTTP
	FinishPasskeyRegistration *finish_passkey_registration.HTTP
	BeginPasskeyLogin         *begin_passkey_login.HTTP
	FinishPasskeyLogin        *finish_passkey_login.HTTP
	BeginPasskeyVerification  *begin_passkey_verification.HTTP
	FinishPasskeyVerification *finish_passkey_verification.HTTP
	VerifyLoginCode           *verify_login_code.HTTP
	StartTwoFASetup           *start_twofa_setup.HTTP
	FinishTwoFASetup          *finish_twofa_setup.HTTP
	DisableTwoFA              *disable_twofa.HTTP
	RegenerateBackupCodes     *regenerate_backup_codes.HTTP
	VerificationRequirements  *get_verification_requirements.HTTP
	VerifySecurityInput       *verify_security_input.HTTP
	RefreshSession            *refresh_session.HTTP
	Logout                    *logout.HTTP
	ListSessions              *list_sessions.HTTP
	RevokeSession             *revoke_session.HTTP
	RevokeOtherSessions       *revoke_other_sessions.HTTP
	RegisterPassword          *register_password.HTTP
	PasswordLogin             *password_login.HTTP
	SendVerificationEmail     *send_verification_email.HTTP
	RequestPasswordReset      *request_password_reset.HTTP
	RecoverPassword           *recover_password.HTTP
}

// NewIdentityErrors builds the identity error writer. The predicates recognize
// errors whose values are owned outside identity: the e-mail uniqueness of
// accounts, WebAuthn protocol errors and missing GORM records.
func NewIdentityErrors() identitytransport.Writer {
	return identitytransport.NewWriter(identitytransport.Foreign{
		EmailTaken: func(err error) bool { return errors.Is(err, model.ErrEmailAlreadyTaken) },
		ProtocolError: func(err error) bool {
			var protocolError *protocol.Error
			return errors.As(err, &protocolError)
		},
		RecordMissing: func(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) },
	}, logger.LogError)
}

// proofIdentity requires a security proof for scope and returns the session
// identity; the proof check writes its own response on failure.
func proofIdentity(scope string) func(*gin.Context) (identitydomain.AuthIdentity, bool) {
	return func(c *gin.Context) (identitydomain.AuthIdentity, bool) {
		if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: scope}) == nil {
			return identitydomain.AuthIdentity{}, false
		}
		identity, _ := middleware.GetSessionAuthIdentity(c)
		return identity, true
	}
}

func NewIdentityHTTP(e IdentityEffects) IdentityHTTP {
	errs := NewIdentityErrors()
	management := sessionadapter.Management{ListFunc: service.ListLoginSessions}
	passkeyEnabled := func() bool { return system_setting.GetPasskeySettings().Enabled }
	authenticatedUserID := func(c *gin.Context) (int, error) {
		user, err := identityhttp.GetAuthenticatedUser(c)
		if err != nil {
			return 0, err
		}
		return user.Id, nil
	}
	return IdentityHTTP{
		Errors: errs,
		GetOAuthBindings: get_oauth_bindings.NewHTTP(oauthadapter.Bindings{}, func(c *gin.Context) get_oauth_bindings.AdminReader {
			return oauthadapter.Bindings{Role: func() int { return c.GetInt("role") }}
		}),
		UnlinkOAuthAccount: unlink_oauth_account.NewHTTP(middleware.GetSessionAuthIdentity, func(c *gin.Context, context []byte) bool {
			return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountUnbind, Context: context}) != nil
		}, func(input unlink_oauth_account.Input) unlink_oauth_account.Completion {
			return &oauthunlink.UnlinkConfirmation{Identity: input.Identity, ProviderID: input.ProviderID, UnlinkFunc: service.UnbindAccountOAuth, Notify: service.NotifyAccountSecurityChange}
		}, errs.WriteError, e.SecurityAudit),
		AdminUnlinkOAuthAccount: admin_unlink_oauth_account.NewHTTP(func(c *gin.Context) admin_unlink_oauth_account.Operation {
			return oauthadapter.Bindings{Role: func() int { return c.GetInt("role") }, ReadProviderID: func() (int, error) { return strconv.Atoi(c.Param("provider_id")) }}
		}),
		LoginWeChat: login_wechat.NewHTTP(func() login_wechat.Completion { return &wechatadapter.Login{} }, e.CompleteOAuthLogin),
		BindWeChat: bind_wechat.NewHTTP(middleware.GetSessionAuthIdentity, func(c *gin.Context, context []byte) bool {
			return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: context}) != nil
		}, func(input bind_wechat.Input) bind_wechat.Completion {
			return &wechatadapter.Binding{Identity: input.Identity}
		}, errs.WriteError, e.SecurityAudit),
		PasswordEncryptionKey: get_password_encryption_key.NewHTTP(passwordlogin.EncryptionKey{}),
		PrimaryLogin:          identityhttp.PrimaryLogin{Audit: e.LoginAudit, Profile: e.BuildProfile, Errors: errs},
		CompleteOAuth: complete_oauth.NewHTTP(func(c *gin.Context) complete_oauth.Completion {
			return &oauthadapter.Callback{Context: c, Name: c.Param("provider")}
		}, e.SecurityAudit, e.CompleteOAuthLogin, func(c *gin.Context, err error) { identityhttp.WriteOAuthError(c, err, errs.WriteError) }, errs.WriteError),
		StartOAuthFlow: start_oauth_flow.NewHTTP(func(c *gin.Context, input *start_oauth_flow.Request) start_oauth_flow.Completion {
			return &oauthadapter.Start{Context: c, Input: input}
		}, e.SecurityAudit, errs.WriteError),
		DeleteAccount: selfdelete.NewHTTP(accountsidentity.Deletion{}, proofIdentity(service.VerificationScopeAccountDelete), service.ClearRefreshCookie, errs.WriteError, e.SecurityAudit),
		StartEmailBinding: start_email_binding.NewHTTP(middleware.GetSessionAuthIdentity, service.ValidateAccountEmail, func(c *gin.Context, context []byte) *identitydomain.FlowAuthorization {
			return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: context})
		}, service.StartEmailBinding, errs.WriteError, e.SecurityAudit),
		ResendEmailBinding: resend_email_binding.NewHTTP(middleware.GetSessionAuthIdentity, service.ResendAccountEmailBinding, errs.WriteError, e.SecurityAudit),
		ConfirmEmailBinding: confirm_email_binding.NewHTTP(middleware.GetSessionAuthIdentity, func(identity identitydomain.AuthIdentity, input confirm_email_binding.Request) confirm_email_binding.Completion {
			return &emailadapter.Confirmation{Identity: identity, Token: input.FlowToken, NewCode: input.NewCode, OldCode: input.OldCode, Finish: service.FinishEmailBinding, Notify: service.NotifyAccountSecurityChange}
		}, errs.WriteError, e.SecurityAudit),
		BeginLoginPasskey: begin_login_passkey.NewHTTP(func(c *gin.Context, input begin_login_passkey.Request) begin_login_passkey.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, ParentFlowToken: input.FlowToken}
		}, errs.WriteError),
		FinishLoginPasskey: finish_login_passkey.NewHTTP(func(c *gin.Context, input finish_login_passkey.Request) finish_login_passkey.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, ParentFlowToken: input.FlowToken, FlowToken: input.PasskeyFlowToken, RawAssertion: input.Credential, Metadata: func() (string, string) { return c.ClientIP(), c.Request.UserAgent() }}
		}, e.CompleteVerifiedLogin, errs.WriteError),
		GetTwoFAStatus:   get_twofa_status.NewHTTP(factors.TwoFAStatus{}, errs.WriteError),
		GetTwoFAStats:    get_twofa_stats.NewHTTP(factors.TwoFAStats{}, errs.WriteError),
		GetPasskeyStatus: get_passkey_status.NewHTTP(factors.PasskeyStatus{}, authenticatedUserID, errs.WriteError),
		DeletePasskey: delete_passkey.NewHTTP(authenticatedUserID, func(c *gin.Context) bool {
			return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePasskeyDelete}) != nil
		}, middleware.GetSessionAuthIdentity, func(input delete_passkey.Input) delete_passkey.Completion {
			return &factors.DeletePasskey{Identity: input.Identity, Advance: service.AdvanceCurrentSessionToUserVersion}
		}, errs.WriteError, e.SecurityAudit),
		AdminDisableTwoFA: admin_disable_twofa.NewHTTP(func(c *gin.Context) admin_disable_twofa.Operation {
			return factors.AdminTwoFA{Role: func() int { return c.GetInt("role") }}
		}, e.ManageAudit, errs.WriteError),
		AdminResetPasskey: admin_reset_passkey.NewHTTP(func(c *gin.Context) admin_reset_passkey.Operation {
			return factors.AdminPasskey{Role: func() int { return c.GetInt("role") }}
		}, e.ManageAudit, errs.WriteError),
		BeginPasskeyRegistration: begin_passkey_registration.NewHTTP(passkeyEnabled, identityhttp.GetAuthenticatedUser, func(c *gin.Context) *model.AuthFlowAuthorization {
			return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePasskeyRegister})
		}, func(c *gin.Context, user *model.User, authorization *model.AuthFlowAuthorization) begin_passkey_registration.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, User: user, Authorization: authorization}
		}, errs.WriteError),
		FinishPasskeyRegistration: finish_passkey_registration.NewHTTP(passkeyEnabled, identityhttp.GetAuthenticatedUser, func(c *gin.Context, user *model.User, request finish_passkey_registration.Request) (finish_passkey_registration.Completion, error) {
			parsed, err := protocol.ParseCredentialCreationResponseBytes(request.Credential)
			if err != nil {
				return nil, err
			}
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, User: user, FlowToken: request.FlowToken, ParsedCreation: parsed}, nil
		}, e.SecurityAudit, errs.WriteError),
		BeginPasskeyLogin: begin_passkey_login.NewHTTP(passkeyEnabled, func(c *gin.Context) begin_passkey_login.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }}
		}, errs.WriteError),
		FinishPasskeyLogin: finish_passkey_login.NewHTTP(passkeyEnabled, func(c *gin.Context, request finish_passkey_login.Request) (finish_passkey_login.Completion, error) {
			parsed, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
			if err != nil {
				return nil, err
			}
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, FlowToken: request.FlowToken, ParsedAssertion: parsed}, nil
		}, e.CompletePasskeyLogin, errs.WriteError),
		BeginPasskeyVerification: begin_passkey_verification.NewHTTP(passkeyEnabled, identityhttp.GetAuthenticatedUser, func(c *gin.Context, user *model.User, request begin_passkey_verification.Request) begin_passkey_verification.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, User: user, VerificationInput: request}
		}, errs.WriteError),
		FinishPasskeyVerification: finish_passkey_verification.NewHTTP(passkeyEnabled, identityhttp.GetAuthenticatedUser, func(c *gin.Context, user *model.User, request finish_passkey_verification.Request) (finish_passkey_verification.Completion, error) {
			parsed, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
			if err != nil {
				return nil, err
			}
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, User: user, FlowToken: request.FlowToken, ParsedAssertion: parsed}, nil
		}, e.SecurityAudit, errs.WriteError),
		VerifyLoginCode: verify_login_code.NewHTTP(service.VerifyLoginCode, e.CompleteVerifiedLogin, errs.WriteError),
		StartTwoFASetup: start_twofa_setup.NewHTTP(func(c *gin.Context) (identitydomain.AuthIdentity, *identitydomain.FlowAuthorization, bool) {
			authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeTwoFASetup})
			if authorization == nil {
				return identitydomain.AuthIdentity{}, nil, false
			}
			identity, _ := middleware.GetSessionAuthIdentity(c)
			return identity, authorization, true
		}, service.StartTwoFASetup, errs.WriteError, e.SecurityAudit),
		FinishTwoFASetup: finish_twofa_setup.NewHTTP(middleware.GetSessionAuthIdentity, func(identity identitydomain.AuthIdentity, input finish_twofa_setup.Verify2FARequest) finish_twofa_setup.Completion {
			return &factors.Enable{Identity: identity, FlowToken: input.FlowToken, Code: input.Code, Finish: service.FinishTwoFASetup, Advance: service.AdvanceCurrentSessionToUserVersion}
		}, errs.WriteError, e.SecurityAudit, identitytransport.RotationData),
		DisableTwoFA: disable_twofa.NewHTTP(proofIdentity(service.VerificationScopeTwoFADisable), func(input disable_twofa.Input) disable_twofa.Completion {
			return &factors.Disable{Identity: input.Identity, Advance: service.AdvanceCurrentSessionToUserVersion}
		}, errs.WriteError, e.SecurityAudit, identitytransport.RotationData),
		RegenerateBackupCodes: regenerate_backup_codes.NewHTTP(proofIdentity(service.VerificationScopeTwoFABackupCodes), func(input regenerate_backup_codes.Input) regenerate_backup_codes.Completion {
			return &factors.BackupCodes{Identity: input.Identity, Advance: service.AdvanceCurrentSessionToUserVersion}
		}, errs.WriteError, e.SecurityAudit, identitytransport.RotationData),
		VerificationRequirements: get_verification_requirements.NewHTTP(middleware.GetSessionAuthIdentity, service.GetVerificationRequirements, errs.WriteError),
		VerifySecurityInput:      verify_security_input.NewHTTP(middleware.GetSessionAuthIdentity, service.VerifySecurityInput, errs.WriteError, e.SecurityAudit),
		RefreshSession: refresh_session.NewHTTP(func(raw, sid, ip, ua string) (refresh_session.Output, error) {
			bundle, user, err := service.RefreshLoginSession(raw, sid, ip, ua)
			if err != nil {
				return refresh_session.Output{}, err
			}
			return refresh_session.Output{Bundle: bundle, Profile: func() map[string]any { return e.BuildProfile(user) }}, nil
		}, service.RefreshCookieName, service.ClearRefreshCookie, service.WriteRefreshCookie, errs.WriteSessionError),
		Logout:                logout.NewHTTP(sessionadapter.Logout{RefreshSIDFunc: service.RefreshTokenSID, ParseAccessFunc: service.ParseAccessToken, RevokeRefreshFunc: service.RevokeByRefreshToken}, service.RefreshCookieName, service.ClearRefreshCookie, errs.WriteSessionError),
		ListSessions:          list_sessions.NewHTTP(management, middleware.GetSessionAuthIdentity, errs.WriteSessionError),
		RevokeSession:         revoke_session.NewHTTP(management, middleware.GetSessionAuthIdentity, service.RefreshCookieName, service.RefreshTokenSID, service.ClearRefreshCookie, errs.WriteSessionError),
		RevokeOtherSessions:   revoke_other_sessions.NewHTTP(management, middleware.GetSessionAuthIdentity, errs.WriteSessionError),
		RegisterPassword:      register_password.NewHTTP(func() bool { return common.RegisterEnabled }, func() bool { return common.PasswordRegisterEnabled }, registration.Operation{}),
		PasswordLogin:         password_login.NewHTTP(func() bool { return common.PasswordLoginEnabled }, passwordlogin.Login{}, e.CompletePasswordLogin),
		SendVerificationEmail: send_verification_email.NewHTTP(recovery.EmailVerification{}, errs.WriteError),
		RequestPasswordReset: request_password_reset.NewHTTP(func(ctx context.Context) request_password_reset.PasswordResetEmailOperation {
			return recovery.PasswordResetEmail{Context: ctx}
		}),
		RecoverPassword: recover_password.NewHTTP(recovery.PasswordRecovery{}),
	}
}
