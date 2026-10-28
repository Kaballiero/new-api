package app

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	accountdomain "github.com/QuantumNous/new-api/internal/accounts/domain"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	accountsidentity "github.com/QuantumNous/new-api/internal/adapter/identity/accounts"
	"github.com/QuantumNous/new-api/internal/adapter/identity/authtoken"
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
	"github.com/QuantumNous/new-api/internal/app/accountscore"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/internal/app/identitycore"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/identity/admin_disable_twofa"
	"github.com/QuantumNous/new-api/internal/identity/admin_reset_passkey"
	"github.com/QuantumNous/new-api/internal/identity/admin_unlink_oauth_account"
	"github.com/QuantumNous/new-api/internal/identity/begin_login_passkey"
	"github.com/QuantumNous/new-api/internal/identity/begin_passkey_login"
	"github.com/QuantumNous/new-api/internal/identity/begin_passkey_registration"
	"github.com/QuantumNous/new-api/internal/identity/begin_passkey_verification"
	"github.com/QuantumNous/new-api/internal/identity/bind_wechat"
	"github.com/QuantumNous/new-api/internal/identity/complete_oauth"
	"github.com/QuantumNous/new-api/internal/identity/complete_primary_login"
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
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"gorm.io/gorm"
)

type IdentityEffects struct {
	LoginAudit            func(*accountrecord.User, *gin.Context)
	CompleteOAuthLogin    func(*accountrecord.User, *gin.Context)
	ManageAudit           func(*gin.Context, int, string, map[string]any)
	CompletePasskeyLogin  func(*gin.Context, *accountrecord.User, int64)
	CompleteVerifiedLogin func(*gin.Context, *identitydto.AuthBundle, string)
	SecurityAudit         func(*gin.Context, int, string, map[string]any)
	BuildProfile          func(*accountrecord.User) map[string]any
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
		EmailTaken: func(err error) bool { return errors.Is(err, accountdomain.ErrEmailAlreadyTaken) },
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
		if middleware.RequireSecurityProof(c, identitydomain.VerificationOperation{Scope: scope}) == nil {
			return identitydomain.AuthIdentity{}, false
		}
		identity, _ := middleware.GetSessionAuthIdentity(c)
		return identity, true
	}
}

func NewIdentityHTTP(e IdentityEffects) IdentityHTTP {
	errs := NewIdentityErrors()
	management := sessionadapter.Management{SQL: identitystorage.SQL(), ListFunc: func(id int, sid string) ([]identitydto.LoginSessionView, error) {
		return sessionadapter.ListLoginSessions(identitystorage.SQL(), id, sid)
	}}
	passkeyEnabled := func() bool { return system_setting.GetPasskeySettings().Enabled }
	authenticatedUserID := func(c *gin.Context) (int, error) {
		user, err := identityhttp.GetAuthenticatedUser(accountstorage.SQL(), c)
		if err != nil {
			return 0, err
		}
		return user.Id, nil
	}
	return IdentityHTTP{
		Errors: errs,
		GetOAuthBindings: get_oauth_bindings.NewHTTP(oauthadapter.Bindings{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL()}, func(c *gin.Context) get_oauth_bindings.AdminReader {
			return oauthadapter.Bindings{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), Role: func() int { return c.GetInt("role") }}
		}),
		UnlinkOAuthAccount: unlink_oauth_account.NewHTTP(middleware.GetSessionAuthIdentity, func(c *gin.Context, context []byte) bool {
			return middleware.RequireSecurityProof(c, identitydomain.VerificationOperation{Scope: identitydomain.VerificationScopeAccountUnbind, Context: context}) != nil
		}, func(input unlink_oauth_account.Input) unlink_oauth_account.Completion {
			return &oauthunlink.UnlinkConfirmation{Accounts: accountstorage.SQL(), Identity: input.Identity, ProviderID: input.ProviderID, UnlinkFunc: identitycore.UnbindAccountOAuth, Notify: emailadapter.NotifyAccountSecurityChange}
		}, errs.WriteError, e.SecurityAudit),
		AdminUnlinkOAuthAccount: admin_unlink_oauth_account.NewHTTP(func(c *gin.Context) admin_unlink_oauth_account.Operation {
			return oauthadapter.Bindings{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), Role: func() int { return c.GetInt("role") }, ReadProviderID: func() (int, error) { return strconv.Atoi(c.Param("provider_id")) }}
		}),
		LoginWeChat: login_wechat.NewHTTP(login_wechat.NewUsecase(), func() login_wechat.Completion {
			return &wechatadapter.Login{InsertUser: accountscore.Insert, Accounts: accountstorage.SQL(), Complete: e.CompleteOAuthLogin}
		}),
		BindWeChat: bind_wechat.NewHTTP(middleware.GetSessionAuthIdentity, func(c *gin.Context, context []byte) bool {
			return middleware.RequireSecurityProof(c, identitydomain.VerificationOperation{Scope: identitydomain.VerificationScopeAccountBind, Context: context}) != nil
		}, func(input bind_wechat.Input) bind_wechat.Completion {
			return &wechatadapter.Binding{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), Identity: input.Identity}
		}, errs.WriteError, e.SecurityAudit),
		PasswordEncryptionKey: get_password_encryption_key.NewHTTP(passwordlogin.EncryptionKey{}),
		PrimaryLogin: identityhttp.PrimaryLogin{
			StartVerification:      identitycore.StartLoginVerification,
			CreateSession:          identitycore.CreateLoginSession,
			CreateSessionAtVersion: identitycore.CreateLoginSessionAtAuthVersion,
			SQL:                    identitystorage.SQL(), Accounts: accountstorage.SQL(),
			Usecase: complete_primary_login.NewUsecase(), Audit: e.LoginAudit, Profile: e.BuildProfile, Errors: errs,
		},
		CompleteOAuth: complete_oauth.NewHTTP(complete_oauth.NewUsecase(), func(c *gin.Context) complete_oauth.Completion {
			return &oauthadapter.Callback{
				ValidateAuthorization: identitycore.ValidateFlowAuthorization,
				ValidateIdentity:      validateIdentitySession,
				FinishVerification:    identitycore.FinishOAuthVerification,
				InsertUserWithTx:      accountscore.InsertWithTx,
				FinalizeUser:          accountscore.FinalizeOAuthUserCreation,
				Accounts:              accountstorage.SQL(), SQL: identitystorage.SQL(),
				Context: c, Name: c.Param("provider"), Complete: e.CompleteOAuthLogin,
			}
		}, e.SecurityAudit, func(c *gin.Context, err error) { identityhttp.WriteOAuthError(c, err, errs.WriteError) }, errs.WriteError),
		StartOAuthFlow: start_oauth_flow.NewHTTP(func(c *gin.Context, input *start_oauth_flow.Request) start_oauth_flow.Completion {
			return &oauthadapter.Start{ValidateIdentity: validateIdentitySession, BeginVerification: identitycore.StartOAuthVerification, SQL: identitystorage.SQL(), Context: c, Input: input}
		}, e.SecurityAudit, errs.WriteError),
		DeleteAccount: selfdelete.NewHTTP(accountsidentity.Deletion{Remove: accountscore.DeleteUserForSession}, proofIdentity(identitydomain.VerificationScopeAccountDelete), identityhttp.ClearRefreshCookie, errs.WriteError, e.SecurityAudit),
		StartEmailBinding: start_email_binding.NewHTTP(middleware.GetSessionAuthIdentity, emailadapter.ValidateAccountEmail, func(c *gin.Context, context []byte) *identitydomain.FlowAuthorization {
			return middleware.RequireSecurityProof(c, identitydomain.VerificationOperation{Scope: identitydomain.VerificationScopeAccountBind, Context: context})
		}, identitycore.StartEmailBinding, errs.WriteError, e.SecurityAudit),
		ResendEmailBinding: resend_email_binding.NewHTTP(middleware.GetSessionAuthIdentity, identitycore.ResendAccountEmailBinding, errs.WriteError, e.SecurityAudit),
		ConfirmEmailBinding: confirm_email_binding.NewHTTP(middleware.GetSessionAuthIdentity, func(identity identitydomain.AuthIdentity, input confirm_email_binding.Request) confirm_email_binding.Completion {
			return &emailadapter.Confirmation{Cache: accountstorage.Cache(), Identity: identity, Token: input.FlowToken, NewCode: input.NewCode, OldCode: input.OldCode, Finish: identitycore.FinishEmailBinding, Notify: emailadapter.NotifyAccountSecurityChange}
		}, errs.WriteError, e.SecurityAudit),
		BeginLoginPasskey: begin_login_passkey.NewHTTP(func(c *gin.Context, input begin_login_passkey.Request) begin_login_passkey.Completion {
			op := newIdentityPasskeyCeremony(c, nil)
			op.ParentFlowToken = input.FlowToken
			return op
		}, errs.WriteError),
		FinishLoginPasskey: finish_login_passkey.NewHTTP(func(c *gin.Context, input finish_login_passkey.Request) finish_login_passkey.Completion {
			op := newIdentityPasskeyCeremony(c, nil)
			op.ParentFlowToken, op.FlowToken, op.RawAssertion = input.FlowToken, input.PasskeyFlowToken, input.Credential
			op.Metadata = func() (string, string) { return c.ClientIP(), c.Request.UserAgent() }
			return op
		}, e.CompleteVerifiedLogin, errs.WriteError),
		GetTwoFAStatus:   get_twofa_status.NewHTTP(factors.TwoFAStatus{SQL: identitystorage.SQL()}, errs.WriteError),
		GetTwoFAStats:    get_twofa_stats.NewHTTP(factors.TwoFAStats{SQL: identitystorage.SQL()}, errs.WriteError),
		GetPasskeyStatus: get_passkey_status.NewHTTP(factors.PasskeyStatus{SQL: identitystorage.SQL()}, authenticatedUserID, errs.WriteError),
		DeletePasskey: delete_passkey.NewHTTP(authenticatedUserID, func(c *gin.Context) bool {
			return middleware.RequireSecurityProof(c, identitydomain.VerificationOperation{Scope: identitydomain.VerificationScopePasskeyDelete}) != nil
		}, middleware.GetSessionAuthIdentity, func(input delete_passkey.Input) delete_passkey.Completion {
			return &factors.DeletePasskey{SQL: identitystorage.SQL(), Identity: input.Identity, Advance: identitycore.AdvanceCurrentSessionToUserVersion}
		}, errs.WriteError, e.SecurityAudit),
		AdminDisableTwoFA: admin_disable_twofa.NewHTTP(func(c *gin.Context) admin_disable_twofa.Operation {
			return factors.AdminTwoFA{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), Role: func() int { return c.GetInt("role") }}
		}, e.ManageAudit, errs.WriteError),
		AdminResetPasskey: admin_reset_passkey.NewHTTP(func(c *gin.Context) admin_reset_passkey.Operation {
			return factors.AdminPasskey{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), Role: func() int { return c.GetInt("role") }}
		}, e.ManageAudit, errs.WriteError),
		BeginPasskeyRegistration: begin_passkey_registration.NewHTTP(begin_passkey_registration.NewUsecase(), passkeyEnabled, func(c *gin.Context) (identitydomain.PasskeyAccount, begin_passkey_registration.CompletionFactory, error) {
			user, err := identityhttp.GetAuthenticatedUser(accountstorage.SQL(), c)
			if err != nil {
				return identitydomain.PasskeyAccount{}, nil, err
			}
			facts := identitydomain.PasskeyAccount{ID: user.Id, AuthVersion: user.AuthVersion}
			return facts, func(authorization *identitydomain.FlowAuthorization) begin_passkey_registration.Completion {
				op := newIdentityPasskeyCeremony(c, user)
				op.Authorization = authorization
				return op
			}, nil
		}, func(c *gin.Context) *identitydomain.FlowAuthorization {
			return middleware.RequireSecurityProof(c, identitydomain.VerificationOperation{Scope: identitydomain.VerificationScopePasskeyRegister})
		}, errs.WriteError),
		FinishPasskeyRegistration: finish_passkey_registration.NewHTTP(finish_passkey_registration.NewUsecase(), passkeyEnabled, func(c *gin.Context) (identitydomain.PasskeyAccount, finish_passkey_registration.CompletionFactory, error) {
			user, err := identityhttp.GetAuthenticatedUser(accountstorage.SQL(), c)
			if err != nil {
				return identitydomain.PasskeyAccount{}, nil, err
			}
			facts := identitydomain.PasskeyAccount{ID: user.Id, AuthVersion: user.AuthVersion}
			return facts, func(request finish_passkey_registration.Request) (finish_passkey_registration.Completion, error) {
				parsed, err := protocol.ParseCredentialCreationResponseBytes(request.Credential)
				if err != nil {
					return nil, err
				}
				op := newIdentityPasskeyCeremony(c, user)
				op.FlowToken, op.ParsedCreation = request.FlowToken, parsed
				return op, nil
			}, nil
		}, e.SecurityAudit, errs.WriteError),
		BeginPasskeyLogin: begin_passkey_login.NewHTTP(passkeyEnabled, func(c *gin.Context) begin_passkey_login.Completion {
			return newIdentityPasskeyCeremony(c, nil)
		}, errs.WriteError),
		FinishPasskeyLogin: finish_passkey_login.NewHTTP(finish_passkey_login.NewUsecase(), passkeyEnabled, func(c *gin.Context, request finish_passkey_login.Request) (finish_passkey_login.Completion, error) {
			parsed, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
			if err != nil {
				return nil, err
			}
			op := newIdentityPasskeyCeremony(c, nil)
			op.FlowToken, op.ParsedAssertion = request.FlowToken, parsed
			op.FinishLogin = e.CompletePasskeyLogin
			return op, nil
		}, errs.WriteError),
		BeginPasskeyVerification: begin_passkey_verification.NewHTTP(begin_passkey_verification.NewUsecase(), passkeyEnabled, func(c *gin.Context) (identitydomain.PasskeyAccount, begin_passkey_verification.CompletionFactory, error) {
			user, err := identityhttp.GetAuthenticatedUser(accountstorage.SQL(), c)
			if err != nil {
				return identitydomain.PasskeyAccount{}, nil, err
			}
			facts := identitydomain.PasskeyAccount{ID: user.Id, AuthVersion: user.AuthVersion}
			return facts, func(request begin_passkey_verification.Request) begin_passkey_verification.Completion {
				op := newIdentityPasskeyCeremony(c, user)
				op.VerificationInput = request
				return op
			}, nil
		}, errs.WriteError),
		FinishPasskeyVerification: finish_passkey_verification.NewHTTP(finish_passkey_verification.NewUsecase(), passkeyEnabled, func(c *gin.Context) (identitydomain.PasskeyAccount, finish_passkey_verification.CompletionFactory, error) {
			user, err := identityhttp.GetAuthenticatedUser(accountstorage.SQL(), c)
			if err != nil {
				return identitydomain.PasskeyAccount{}, nil, err
			}
			facts := identitydomain.PasskeyAccount{ID: user.Id, AuthVersion: user.AuthVersion}
			return facts, func(request finish_passkey_verification.Request) (finish_passkey_verification.Completion, error) {
				parsed, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
				if err != nil {
					return nil, err
				}
				op := newIdentityPasskeyCeremony(c, user)
				op.FlowToken, op.ParsedAssertion = request.FlowToken, parsed
				return op, nil
			}, nil
		}, e.SecurityAudit, errs.WriteError),
		VerifyLoginCode: verify_login_code.NewHTTP(identitycore.VerifyLoginCode, e.CompleteVerifiedLogin, errs.WriteError),
		StartTwoFASetup: start_twofa_setup.NewHTTP(func(c *gin.Context) (identitydomain.AuthIdentity, *identitydomain.FlowAuthorization, bool) {
			authorization := middleware.RequireSecurityProof(c, identitydomain.VerificationOperation{Scope: identitydomain.VerificationScopeTwoFASetup})
			if authorization == nil {
				return identitydomain.AuthIdentity{}, nil, false
			}
			identity, _ := middleware.GetSessionAuthIdentity(c)
			return identity, authorization, true
		}, identitycore.StartTwoFASetup, errs.WriteError, e.SecurityAudit),
		FinishTwoFASetup: finish_twofa_setup.NewHTTP(middleware.GetSessionAuthIdentity, func(identity identitydomain.AuthIdentity, input finish_twofa_setup.Verify2FARequest) finish_twofa_setup.Completion {
			return &factors.Enable{Identity: identity, FlowToken: input.FlowToken, Code: input.Code, Finish: identitycore.FinishTwoFASetup, Advance: identitycore.AdvanceCurrentSessionToUserVersion}
		}, errs.WriteError, e.SecurityAudit, identitytransport.RotationData),
		DisableTwoFA: disable_twofa.NewHTTP(proofIdentity(identitydomain.VerificationScopeTwoFADisable), func(input disable_twofa.Input) disable_twofa.Completion {
			return &factors.Disable{SQL: identitystorage.SQL(), Identity: input.Identity, Advance: identitycore.AdvanceCurrentSessionToUserVersion}
		}, errs.WriteError, e.SecurityAudit, identitytransport.RotationData),
		RegenerateBackupCodes: regenerate_backup_codes.NewHTTP(proofIdentity(identitydomain.VerificationScopeTwoFABackupCodes), func(input regenerate_backup_codes.Input) regenerate_backup_codes.Completion {
			return &factors.BackupCodes{SQL: identitystorage.SQL(), Identity: input.Identity, Advance: identitycore.AdvanceCurrentSessionToUserVersion}
		}, errs.WriteError, e.SecurityAudit, identitytransport.RotationData),
		VerificationRequirements: get_verification_requirements.NewHTTP(middleware.GetSessionAuthIdentity, identitycore.GetVerificationRequirements, errs.WriteError),
		VerifySecurityInput:      verify_security_input.NewHTTP(middleware.GetSessionAuthIdentity, identitycore.VerifySecurityInput, errs.WriteError, e.SecurityAudit),
		RefreshSession: refresh_session.NewHTTP(func(raw, sid, ip, ua string) (refresh_session.Output, error) {
			bundle, user, err := identitycore.RefreshLoginSession(raw, sid, ip, ua)
			if err != nil {
				return refresh_session.Output{}, err
			}
			return refresh_session.Output{Bundle: bundle, Profile: func() map[string]any { return e.BuildProfile(user) }}, nil
		}, identitydomain.RefreshCookieName, identityhttp.ClearRefreshCookie, func(c *gin.Context, token string) { identityhttp.WriteRefreshCookie(identitystorage.SQL(), c, token) }, errs.WriteSessionError),
		Logout:                logout.NewHTTP(sessionadapter.Logout{SQL: identitystorage.SQL(), RefreshSIDFunc: sessionadapter.RefreshTokenSID, ParseAccessFunc: authtoken.ParseAccessToken, RevokeRefreshFunc: identitycore.RevokeByRefreshToken}, identitydomain.RefreshCookieName, identityhttp.ClearRefreshCookie, errs.WriteSessionError),
		ListSessions:          list_sessions.NewHTTP(management, middleware.GetSessionAuthIdentity, errs.WriteSessionError),
		RevokeSession:         revoke_session.NewHTTP(management, middleware.GetSessionAuthIdentity, identitydomain.RefreshCookieName, sessionadapter.RefreshTokenSID, identityhttp.ClearRefreshCookie, errs.WriteSessionError),
		RevokeOtherSessions:   revoke_other_sessions.NewHTTP(management, middleware.GetSessionAuthIdentity, errs.WriteSessionError),
		RegisterPassword:      register_password.NewHTTP(func() bool { return common.RegisterEnabled }, func() bool { return common.PasswordRegisterEnabled }, registration.Operation{FindInsertedUserID: accountstorage.SQL().FindInsertedUserID, InsertUser: accountscore.Insert, InsertToken: apikeystorage.Persistence().Insert, Accounts: accountstorage.SQL()}),
		PasswordLogin:         password_login.NewHTTP(func() bool { return common.PasswordLoginEnabled }, passwordlogin.Login{ValidateAndFill: accountscore.ValidateAndFill}, e.CompletePasswordLogin),
		SendVerificationEmail: send_verification_email.NewHTTP(recovery.EmailVerification{Accounts: accountstorage.SQL()}, errs.WriteError),
		RequestPasswordReset: request_password_reset.NewHTTP(func(ctx context.Context) request_password_reset.PasswordResetEmailOperation {
			return recovery.PasswordResetEmail{Accounts: accountstorage.SQL(), Context: ctx}
		}),
		RecoverPassword: recover_password.NewHTTP(recovery.PasswordRecovery{Reset: accountscore.ResetUserPasswordByEmail}),
	}
}

// newIdentityPasskeyCeremony binds request data to the once-built flow orchestration.
func newIdentityPasskeyCeremony(c *gin.Context, user *accountrecord.User) *webauthnadapter.Ceremony {
	return &webauthnadapter.Ceremony{
		ValidateAuthorization: identitycore.ValidateFlowAuthorization,
		AdvanceSessionVersion: identitycore.AdvanceCurrentSessionToUserVersion,
		BindVerification:      identitycore.BindVerificationOperation,
		RequireVerification: func(identity identitydomain.AuthIdentity, scope, method string) error {
			_, err := identitycore.RequireVerificationMethod(identity, scope, method)
			return err
		},
		CompleteVerification:    identitycore.CompleteSecurityVerification,
		LoadLoginVerification:   identitycore.RequireLoginVerification,
		FinishLoginVerification: identitycore.CompleteLoginVerification,
		Accounts:                accountstorage.SQL(), SQL: identitystorage.SQL(),
		Request: c.Request, User: user,
		IdentityReader:        func() (identitydomain.AuthIdentity, bool) { return middleware.GetSessionAuthIdentity(c) },
		CreateSessionDataFlow: identitycore.CreateSessionDataFlow,
		PopSessionDataFlow:    identitycore.PopSessionDataFlow,
	}
}

func validateIdentitySession(identity identitydomain.AuthIdentity) error {
	_, _, err := identitycore.ValidateLoginSession(identity)
	return err
}
