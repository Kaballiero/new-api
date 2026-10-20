package app

import (
	oauthunlink "github.com/QuantumNous/new-api/internal/adapter/identity/oauthunlink"
	"github.com/QuantumNous/new-api/internal/identity/admin_unlink_oauth_account"
	"github.com/QuantumNous/new-api/internal/identity/get_oauth_bindings"
	"github.com/QuantumNous/new-api/internal/identity/unlink_oauth_account"
	"strconv"

	accountsidentity "github.com/QuantumNous/new-api/internal/adapter/identity/accounts"
	emailadapter "github.com/QuantumNous/new-api/internal/adapter/identity/emailbinding"
	oauthadapter "github.com/QuantumNous/new-api/internal/adapter/identity/oauth"
	wechatadapter "github.com/QuantumNous/new-api/internal/adapter/identity/wechat"
	"github.com/QuantumNous/new-api/internal/identity/bind_wechat"
	"github.com/QuantumNous/new-api/internal/identity/complete_oauth"
	"github.com/QuantumNous/new-api/internal/identity/confirm_email_binding"
	selfdelete "github.com/QuantumNous/new-api/internal/identity/delete_account"
	"github.com/QuantumNous/new-api/internal/identity/get_password_encryption_key"
	"github.com/QuantumNous/new-api/internal/identity/login_wechat"
	"github.com/QuantumNous/new-api/internal/identity/resend_email_binding"
	"github.com/QuantumNous/new-api/internal/identity/start_email_binding"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_flow"

	"github.com/QuantumNous/new-api/internal/identity/admin_disable_twofa"
	"github.com/QuantumNous/new-api/internal/identity/admin_reset_passkey"
	"github.com/QuantumNous/new-api/internal/identity/begin_login_passkey"
	"github.com/QuantumNous/new-api/internal/identity/delete_passkey"
	"github.com/QuantumNous/new-api/internal/identity/finish_login_passkey"
	"github.com/QuantumNous/new-api/internal/identity/get_passkey_status"
	"github.com/QuantumNous/new-api/internal/identity/get_twofa_stats"
	"github.com/QuantumNous/new-api/internal/identity/get_twofa_status"

	webauthnadapter "github.com/QuantumNous/new-api/internal/adapter/identity/webauthn"
	"github.com/QuantumNous/new-api/internal/identity/begin_passkey_login"
	"github.com/QuantumNous/new-api/internal/identity/begin_passkey_registration"
	"github.com/QuantumNous/new-api/internal/identity/begin_passkey_verification"
	"github.com/QuantumNous/new-api/internal/identity/finish_passkey_login"
	"github.com/QuantumNous/new-api/internal/identity/finish_passkey_registration"
	"github.com/QuantumNous/new-api/internal/identity/finish_passkey_verification"
	"github.com/go-webauthn/webauthn/protocol"

	"github.com/QuantumNous/new-api/internal/adapter/identity/factors"
	"github.com/QuantumNous/new-api/internal/identity/disable_twofa"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/finish_twofa_setup"
	"github.com/QuantumNous/new-api/internal/identity/regenerate_backup_codes"
	"github.com/QuantumNous/new-api/internal/identity/start_twofa_setup"
	"github.com/QuantumNous/new-api/internal/identity/verify_login_code"

	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	sessionadapter "github.com/QuantumNous/new-api/internal/adapter/identity/session"
	"github.com/QuantumNous/new-api/internal/identity/get_verification_requirements"
	"github.com/QuantumNous/new-api/internal/identity/list_sessions"
	"github.com/QuantumNous/new-api/internal/identity/logout"
	"github.com/QuantumNous/new-api/internal/identity/refresh_session"
	"github.com/QuantumNous/new-api/internal/identity/revoke_other_sessions"
	"github.com/QuantumNous/new-api/internal/identity/revoke_session"
	"github.com/QuantumNous/new-api/internal/identity/verify_security_input"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service"

	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/identity/passwordlogin"
	"github.com/QuantumNous/new-api/internal/adapter/identity/recovery"
	"github.com/QuantumNous/new-api/internal/adapter/identity/registration"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
	"github.com/QuantumNous/new-api/internal/identity/recover_password"
	"github.com/QuantumNous/new-api/internal/identity/register_password"
	"github.com/QuantumNous/new-api/internal/identity/request_password_reset"
	"github.com/QuantumNous/new-api/internal/identity/send_verification_email"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type IdentityEffects struct {
	LoginAudit            func(*model.User, *gin.Context)
	CompleteOAuthLogin    func(*model.User, *gin.Context)
	ManageAudit           func(*gin.Context, int, string, map[string]any)
	CompletePasskeyLogin  func(*gin.Context, *model.User, int64)
	CompleteVerifiedLogin func(*gin.Context, *identitydto.AuthBundle, string)
	SecurityAudit         func(*gin.Context, int, string, map[string]any)
	BuildProfile          func(*model.User) map[string]any
	WriteSecurityError    func(*gin.Context, error)
	CompletePasswordLogin func(*gin.Context, password_login.Account)
}
type IdentityHTTP struct {
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

func NewIdentityHTTP(e IdentityEffects) IdentityHTTP {
	management := sessionadapter.Management{ListFunc: service.ListLoginSessions}
	return IdentityHTTP{
		GetOAuthBindings: get_oauth_bindings.NewHTTP(oauthadapter.Bindings{}, func(c *gin.Context) get_oauth_bindings.AdminReader {
			return oauthadapter.Bindings{Role: func() int { return c.GetInt("role") }}
		}),
		UnlinkOAuthAccount: unlink_oauth_account.NewHTTP(middleware.GetSessionAuthIdentity, func() error { return service.ErrAuthTokenInvalid }, func(c *gin.Context, context []byte) bool {
			return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountUnbind, Context: context}) != nil
		}, func(input unlink_oauth_account.Input) unlink_oauth_account.Completion {
			return &oauthunlink.UnlinkConfirmation{Identity: input.Identity, ProviderID: input.ProviderID, UnlinkFunc: service.UnbindAccountOAuth, Notify: service.NotifyAccountSecurityChange}
		}, identityhttp.WriteSecurityOperationError, e.SecurityAudit),
		AdminUnlinkOAuthAccount: admin_unlink_oauth_account.NewHTTP(func(c *gin.Context) admin_unlink_oauth_account.Operation {
			return oauthadapter.Bindings{Role: func() int { return c.GetInt("role") }, ReadProviderID: func() (int, error) { return strconv.Atoi(c.Param("provider_id")) }}
		}), LoginWeChat: login_wechat.NewHTTP(func() login_wechat.Completion { return &wechatadapter.Login{} }, e.CompleteOAuthLogin), BindWeChat: bind_wechat.NewHTTP(func(input bind_wechat.Input) bind_wechat.Completion {
			return &wechatadapter.Binding{Identity: input.Identity}
		}, e.SecurityAudit), PasswordEncryptionKey: get_password_encryption_key.NewHTTP(passwordlogin.EncryptionKey{}), PrimaryLogin: identityhttp.PrimaryLogin{Audit: e.LoginAudit, Profile: e.BuildProfile}, CompleteOAuth: complete_oauth.NewHTTP(func(c *gin.Context) complete_oauth.Completion {
			return &oauthadapter.Callback{Context: c, Name: c.Param("provider")}
		}, e.SecurityAudit, e.CompleteOAuthLogin, identityhttp.WriteOAuthError), StartOAuthFlow: start_oauth_flow.NewHTTP(func(c *gin.Context, input *start_oauth_flow.Request) start_oauth_flow.Completion {
			return &oauthadapter.Start{Context: c, Input: input}
		}, e.SecurityAudit), DeleteAccount: selfdelete.NewHTTP(accountsidentity.Deletion{}, e.SecurityAudit),
		StartEmailBinding: start_email_binding.NewHTTP(middleware.GetSessionAuthIdentity, func() error { return service.ErrAuthTokenInvalid }, func() error { return service.ErrVerificationContextInvalid }, service.ValidateAccountEmail, func(c *gin.Context, context []byte) *identitydomain.FlowAuthorization {
			return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: context})
		}, service.StartEmailBinding, identityhttp.WriteSecurityOperationError, e.SecurityAudit),
		ResendEmailBinding: resend_email_binding.NewHTTP(middleware.GetSessionAuthIdentity, func() error { return service.ErrAuthTokenInvalid }, func() error { return model.ErrAuthFlowInvalid }, service.ResendAccountEmailBinding, identityhttp.WriteSecurityOperationError, e.SecurityAudit),
		ConfirmEmailBinding: confirm_email_binding.NewHTTP(middleware.GetSessionAuthIdentity, func() error { return service.ErrAuthTokenInvalid }, func() error { return model.ErrAuthFlowInvalid }, func(identity identitydomain.AuthIdentity, input confirm_email_binding.Request) confirm_email_binding.Completion {
			return &emailadapter.Confirmation{Identity: identity, Token: input.FlowToken, NewCode: input.NewCode, OldCode: input.OldCode, Finish: service.FinishEmailBinding, Notify: service.NotifyAccountSecurityChange}
		}, identityhttp.WriteSecurityOperationError, e.SecurityAudit),
		BeginLoginPasskey: begin_login_passkey.NewHTTP(func(c *gin.Context, input begin_login_passkey.Request) begin_login_passkey.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, ParentFlowToken: input.FlowToken}
		}),
		FinishLoginPasskey: finish_login_passkey.NewHTTP(func(c *gin.Context, input finish_login_passkey.Request) finish_login_passkey.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, ParentFlowToken: input.FlowToken, FlowToken: input.PasskeyFlowToken, RawAssertion: input.Credential, Metadata: func() (string, string) { return c.ClientIP(), c.Request.UserAgent() }}
		}, e.CompleteVerifiedLogin),
		GetTwoFAStatus: get_twofa_status.NewHTTP(factors.TwoFAStatus{}), GetTwoFAStats: get_twofa_stats.NewHTTP(factors.TwoFAStats{}), GetPasskeyStatus: get_passkey_status.NewHTTP(factors.PasskeyStatus{}),
		DeletePasskey: delete_passkey.NewHTTP(func(input delete_passkey.Input) delete_passkey.Completion {
			return &factors.DeletePasskey{Identity: input.Identity, Advance: service.AdvanceCurrentSessionToUserVersion}
		}, e.SecurityAudit),
		AdminDisableTwoFA: admin_disable_twofa.NewHTTP(func(c *gin.Context) admin_disable_twofa.Operation {
			return factors.AdminTwoFA{Role: func() int { return c.GetInt("role") }}
		}, e.ManageAudit),
		AdminResetPasskey: admin_reset_passkey.NewHTTP(func(c *gin.Context) admin_reset_passkey.Operation {
			return factors.AdminPasskey{Role: func() int { return c.GetInt("role") }}
		}, e.ManageAudit), BeginPasskeyRegistration: begin_passkey_registration.NewHTTP(func(c *gin.Context, user *model.User, authorization *model.AuthFlowAuthorization) begin_passkey_registration.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, User: user, Authorization: authorization}
		}),
		FinishPasskeyRegistration: finish_passkey_registration.NewHTTP(func(c *gin.Context, user *model.User, request finish_passkey_registration.Request, parsed *protocol.ParsedCredentialCreationData) finish_passkey_registration.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, User: user, FlowToken: request.FlowToken, ParsedCreation: parsed}
		}, e.SecurityAudit),
		BeginPasskeyLogin: begin_passkey_login.NewHTTP(func(c *gin.Context) begin_passkey_login.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }}
		}),
		FinishPasskeyLogin: finish_passkey_login.NewHTTP(func(c *gin.Context, request finish_passkey_login.Request, parsed *protocol.ParsedCredentialAssertionData) finish_passkey_login.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, FlowToken: request.FlowToken, ParsedAssertion: parsed}
		}, e.CompletePasskeyLogin),
		BeginPasskeyVerification: begin_passkey_verification.NewHTTP(func(c *gin.Context, user *model.User, request begin_passkey_verification.Request) begin_passkey_verification.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, User: user, VerificationInput: request}
		}),
		FinishPasskeyVerification: finish_passkey_verification.NewHTTP(func(c *gin.Context, user *model.User, request finish_passkey_verification.Request, parsed *protocol.ParsedCredentialAssertionData) finish_passkey_verification.Completion {
			return &webauthnadapter.Ceremony{Request: c.Request, IdentityReader: func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) }, User: user, FlowToken: request.FlowToken, ParsedAssertion: parsed}
		}, e.SecurityAudit), VerifyLoginCode: verify_login_code.NewHTTP(service.VerifyLoginCode, e.CompleteVerifiedLogin, identityhttp.WriteSecurityOperationError, func() error { return service.ErrProofMethod }),
		StartTwoFASetup: start_twofa_setup.NewHTTP(func(c *gin.Context) (identitydomain.AuthIdentity, *identitydomain.FlowAuthorization, bool) {
			authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeTwoFASetup})
			if authorization == nil {
				return identitydomain.AuthIdentity{}, nil, false
			}
			identity, _ := middleware.GetSessionAuthIdentity(c)
			return identity, authorization, true
		}, service.StartTwoFASetup, identityhttp.WriteSecurityOperationError, e.SecurityAudit),
		FinishTwoFASetup: finish_twofa_setup.NewHTTP(middleware.GetSessionAuthIdentity, func() error { return service.ErrAuthTokenInvalid }, func(identity identitydomain.AuthIdentity, input finish_twofa_setup.Verify2FARequest) finish_twofa_setup.Completion {
			return &factors.Enable{Identity: identity, FlowToken: input.FlowToken, Code: input.Code, Finish: service.FinishTwoFASetup, Advance: service.AdvanceCurrentSessionToUserVersion}
		}, identityhttp.WriteSecurityOperationError, e.SecurityAudit, identityhttp.AuthRotationData),
		DisableTwoFA: disable_twofa.NewHTTP(func(c *gin.Context) (identitydomain.AuthIdentity, bool) {
			if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeTwoFADisable}) == nil {
				return identitydomain.AuthIdentity{}, false
			}
			identity, _ := middleware.GetSessionAuthIdentity(c)
			return identity, true
		}, func(input disable_twofa.Input) disable_twofa.Completion {
			return &factors.Disable{Identity: input.Identity, Advance: service.AdvanceCurrentSessionToUserVersion}
		}, identityhttp.WriteSecurityOperationError, e.SecurityAudit, identityhttp.AuthRotationData),
		RegenerateBackupCodes: regenerate_backup_codes.NewHTTP(func(c *gin.Context) (identitydomain.AuthIdentity, bool) {
			if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeTwoFABackupCodes}) == nil {
				return identitydomain.AuthIdentity{}, false
			}
			identity, _ := middleware.GetSessionAuthIdentity(c)
			return identity, true
		}, func(input regenerate_backup_codes.Input) regenerate_backup_codes.Completion {
			return &factors.BackupCodes{Identity: input.Identity, Advance: service.AdvanceCurrentSessionToUserVersion}
		}, identityhttp.WriteSecurityOperationError, e.SecurityAudit, identityhttp.AuthRotationData), VerificationRequirements: get_verification_requirements.NewHTTP(middleware.GetSessionAuthIdentity, service.GetVerificationRequirements, identityhttp.WriteSecurityOperationError), VerifySecurityInput: verify_security_input.NewHTTP(middleware.GetSessionAuthIdentity, service.VerifySecurityInput, identityhttp.WriteSecurityOperationError, e.SecurityAudit),
		RefreshSession: refresh_session.NewHTTP(func(raw, sid, ip, ua string) (refresh_session.Output, error) {
			bundle, user, err := service.RefreshLoginSession(raw, sid, ip, ua)
			if err != nil {
				return refresh_session.Output{}, err
			}
			return refresh_session.Output{Bundle: bundle, Profile: func() map[string]any { return e.BuildProfile(user) }}, nil
		}, service.RefreshCookieName, service.ClearRefreshCookie, service.WriteRefreshCookie, identityhttp.WriteAuthSessionError, func() error { return service.ErrRefreshTokenInvalid }, func() error { return service.ErrLoginSessionRevoked }),
		Logout:       logout.NewHTTP(sessionadapter.Logout{RefreshSIDFunc: service.RefreshTokenSID, ParseAccessFunc: service.ParseAccessToken, RevokeRefreshFunc: service.RevokeByRefreshToken, MismatchError: func() error { return service.ErrLoginSessionMismatch }}, service.RefreshCookieName, service.ClearRefreshCookie, identityhttp.WriteAuthSessionError),
		ListSessions: list_sessions.NewHTTP(management), RevokeSession: revoke_session.NewHTTP(management), RevokeOtherSessions: revoke_other_sessions.NewHTTP(management), RegisterPassword: register_password.NewHTTP(func() bool { return common.RegisterEnabled }, func() bool { return common.PasswordRegisterEnabled }, registration.Operation{}), PasswordLogin: password_login.NewHTTP(func() bool { return common.PasswordLoginEnabled }, passwordlogin.Login{}, e.CompletePasswordLogin),
		SendVerificationEmail: send_verification_email.NewHTTP(recovery.EmailVerification{}, e.WriteSecurityError),
		RequestPasswordReset: request_password_reset.NewHTTP(func(ctx context.Context) request_password_reset.PasswordResetEmailOperation {
			return recovery.PasswordResetEmail{Context: ctx}
		}, func() error { return model.ErrEmailNotFound }),
		RecoverPassword: recover_password.NewHTTP(recovery.PasswordRecovery{}, recover_password.PasswordRecoveryErrors{EmailNotFound: func() error { return model.ErrEmailNotFound }, EmailAmbiguous: func() error { return model.ErrEmailAmbiguous }}),
	}
}
