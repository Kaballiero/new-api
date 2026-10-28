package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errForeignEmailTaken = errors.New("email already taken")
	errForeignProtocol   = errors.New("webauthn protocol error")
	errForeignMissing    = errors.New("record not found")
)

func testWriter(logged *[]string) Writer {
	return NewWriter(Foreign{
		EmailTaken:    func(err error) bool { return errors.Is(err, errForeignEmailTaken) },
		ProtocolError: func(err error) bool { return errors.Is(err, errForeignProtocol) },
		RecordMissing: func(err error) bool { return errors.Is(err, errForeignMissing) },
	}, func(_ context.Context, message string) { *logged = append(*logged, message) })
}

func writeTestError(t *testing.T, write func(*gin.Context, error), err error, provider, lang string) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/test", nil)
	if provider != "" {
		c.Params = gin.Params{{Key: "provider", Value: provider}}
	}
	if lang != "" {
		c.Set(string(constant.ContextKeyLanguage), lang)
	}
	write(c, err)
	return recorder, c
}

// The expectations repeat the legacy WriteSecurityOperationError and
// WritePasskeyFailure responses: status, code, English message and the
// security_error_code context value.
func TestWriteErrorKeepsLegacyResponses(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name         string
		err          error
		provider     string
		status       int
		body         string
		securityCode string
	}{
		{"passkey identity", domain.ErrPasskeyIdentityRequired, "", http.StatusOK, `{"success":false,"message":"The current authentication method does not support security verification"}`, ""},
		{"passkey missing", domain.ErrPasskeyMissing, "", http.StatusOK, `{"success":false,"message":"This user has not bound a Passkey"}`, ""},
		{"passkey record", domain.ErrPasskeyRecordInvalid, "", http.StatusOK, `{"success":false,"message":"Unable to create Passkey credential"}`, ""},
		{"passkey login", domain.ErrPasskeyLoginInvalid, "", http.StatusOK, `{"success":false,"message":"Passkey login status is abnormal"}`, ""},
		{"passkey disabled", domain.ErrPasskeyAccountDisabled, "", http.StatusOK, `{"success":false,"message":"This user has been disabled"}`, ""},
		{"email invalid", domain.ErrEmailInvalid, "", http.StatusOK, `{"success":false,"code":"EMAIL_ADDRESS_REJECTED","message":"Please enter a valid email address"}`, "EMAIL_ADDRESS_REJECTED"},
		{"email restricted", domain.ErrEmailRestricted, "", http.StatusOK, `{"success":false,"code":"EMAIL_ADDRESS_REJECTED","message":"This email address is not allowed by the administrator's email policy."}`, "EMAIL_ADDRESS_REJECTED"},
		{"email taken", fmt.Errorf("update: %w", errForeignEmailTaken), "", http.StatusOK, `{"success":false,"code":"EMAIL_ALREADY_TAKEN","message":"This email address is already in use."}`, "EMAIL_ALREADY_TAKEN"},
		{"email delivery", domain.ErrEmailBindingDelivery, "", http.StatusOK, `{"success":false,"code":"EMAIL_BINDING_DELIVERY_FAILED","message":"Verification email could not be sent. Start email verification again."}`, "EMAIL_BINDING_DELIVERY_FAILED"},
		{"email code", domain.ErrEmailBindingCodeInvalid, "", http.StatusOK, `{"success":false,"code":"EMAIL_BINDING_CODE_INVALID","message":"Email verification code is incorrect."}`, "EMAIL_BINDING_CODE_INVALID"},
		{"email locked", domain.ErrEmailBindingLocked, "", http.StatusOK, `{"success":false,"code":"EMAIL_BINDING_LOCKED","message":"Too many incorrect codes. Start email verification again."}`, "EMAIL_BINDING_LOCKED"},
		{"email resend", domain.ErrEmailBindingResendWait, "", http.StatusTooManyRequests, `{"success":false,"code":"EMAIL_BINDING_RESEND_WAIT","message":"Please wait before requesting another verification code."}`, "EMAIL_BINDING_RESEND_WAIT"},
		{"password length", common.ErrAccountPasswordLength, "", http.StatusOK, `{"success":false,"code":"PASSWORD_POLICY_REJECTED","message":"Password must contain between 8 and 128 characters."}`, "PASSWORD_POLICY_REJECTED"},
		{"password same", domain.ErrPasswordSame, "", http.StatusOK, `{"success":false,"code":"PASSWORD_POLICY_REJECTED","message":"New password must be different from current password"}`, "PASSWORD_POLICY_REJECTED"},
		{"password legacy", common.ErrPasswordLegacyLimit, "", http.StatusOK, `{"success":false,"code":"PASSWORD_POLICY_REJECTED","message":"Long passwords are unavailable until the password storage upgrade is complete."}`, "PASSWORD_POLICY_REJECTED"},
		{"current password", domain.ErrCurrentPasswordInvalid, "", http.StatusOK, `{"success":false,"code":"CURRENT_PASSWORD_INVALID","message":"Current password is incorrect."}`, "CURRENT_PASSWORD_INVALID"},
		{"password state", domain.ErrPasswordState, "", http.StatusConflict, `{"success":false,"code":"ACCOUNT_SECURITY_STATE_CHANGED","message":"Password settings have changed. Reload the page and try again."}`, "ACCOUNT_SECURITY_STATE_CHANGED"},
		{"binding changed", domain.ErrBindingChanged, "", http.StatusConflict, `{"success":false,"code":"ACCOUNT_SECURITY_STATE_CHANGED","message":"Account bindings have changed. Start this operation again."}`, "ACCOUNT_SECURITY_STATE_CHANGED"},
		{"last login method", domain.ErrLastLoginMethod, "", http.StatusOK, `{"success":false,"code":"LAST_LOGIN_METHOD","message":"Add another login method before unlinking this account."}`, "LAST_LOGIN_METHOD"},
		{"telegram config", domain.ErrTelegramOAuthNotConfigured, "", http.StatusOK, `{"success":false,"code":"TELEGRAM_OAUTH_NOT_CONFIGURED","message":"Telegram OAuth is not configured or enabled. Please contact your administrator."}`, "TELEGRAM_OAUTH_NOT_CONFIGURED"},
		{"telegram conflict", domain.ErrTelegramOAuthConflict, "", http.StatusOK, `{"success":false,"code":"TELEGRAM_OAUTH_CONFLICT","message":"The telegram OAuth provider name is reserved. Ask your administrator to rename the conflicting custom provider."}`, "TELEGRAM_OAUTH_CONFLICT"},
		{"telegram failed", fmt.Errorf("%w: token endpoint status 500", domain.ErrTelegramOAuthFailed), "", http.StatusOK, `{"success":false,"code":"TELEGRAM_OAUTH_FAILED","message":"Telegram authorization failed. Please try again."}`, "TELEGRAM_OAUTH_FAILED"},
		{"telegram unbound", domain.ErrTelegramAccountNotBound, "", http.StatusOK, `{"success":false,"code":"TELEGRAM_ACCOUNT_NOT_BOUND","message":"This Telegram account is not linked. Sign in using another method and link it first."}`, "TELEGRAM_ACCOUNT_NOT_BOUND"},
		{"identity claimed", domain.ErrIdentityClaimed, "github", http.StatusOK, `{"success":false,"code":"ACCOUNT_ALREADY_BOUND","message":"This external account is already bound."}`, "ACCOUNT_ALREADY_BOUND"},
		{"telegram claimed", domain.ErrIdentityClaimed, "telegram", http.StatusOK, `{"success":false,"code":"TELEGRAM_BIND_ALREADY_BOUND","message":"This Telegram account is already bound."}`, "TELEGRAM_BIND_ALREADY_BOUND"},
		{"context invalid", domain.ErrVerificationContextInvalid, "", http.StatusBadRequest, `{"success":false,"code":"SECURITY_CONTEXT_INVALID","message":"The action details are invalid."}`, "SECURITY_CONTEXT_INVALID"},
		{"forbidden", domain.ErrVerificationForbidden, "", http.StatusForbidden, `{"success":false,"code":"SECURITY_ACTION_FORBIDDEN","message":"You do not have permission to perform this action."}`, "SECURITY_ACTION_FORBIDDEN"},
		{"verification failed", domain.ErrVerificationFailed, "", http.StatusOK, `{"success":false,"code":"SECURITY_VERIFICATION_FAILED","message":"Verification failed. Please try again."}`, "SECURITY_VERIFICATION_FAILED"},
		{"webauthn protocol", errForeignProtocol, "", http.StatusOK, `{"success":false,"code":"SECURITY_VERIFICATION_FAILED","message":"Verification failed. Please try again."}`, "SECURITY_VERIFICATION_FAILED"},
		{"locked", domain.ErrVerificationLocked, "", http.StatusOK, `{"success":false,"code":"SECURITY_VERIFICATION_LOCKED","message":"Two-factor authentication is temporarily locked."}`, "SECURITY_VERIFICATION_LOCKED"},
		{"unavailable", domain.ErrVerificationUnavailable, "", http.StatusOK, `{"success":false,"code":"SECURITY_METHOD_UNAVAILABLE","message":"This verification method is currently unavailable."}`, "SECURITY_METHOD_UNAVAILABLE"},
		{"flow required", domain.ErrVerificationFlowRequired, "", http.StatusBadRequest, `{"success":false,"code":"SECURITY_VERIFICATION_FLOW_REQUIRED","message":"This verification method requires its dedicated verification flow."}`, "SECURITY_VERIFICATION_FLOW_REQUIRED"},
		{"proof method", domain.ErrProofMethod, "", http.StatusOK, `{"success":false,"code":"SECURITY_PROOF_METHOD_MISMATCH","message":"This verification method is not allowed for this action."}`, "SECURITY_PROOF_METHOD_MISMATCH"},
		{"proof scope", domain.ErrProofScope, "", http.StatusOK, `{"success":false,"code":"SECURITY_PROOF_SCOPE_MISMATCH","message":"Verification does not match this action."}`, "SECURITY_PROOF_SCOPE_MISMATCH"},
		{"oauth mismatch", domain.ErrOAuthAccountMismatch, "", http.StatusOK, `{"success":false,"code":"OAUTH_ACCOUNT_MISMATCH","message":"The OAuth account does not match the account linked to your profile."}`, "OAUTH_ACCOUNT_MISMATCH"},
		{"twofa setup", domain.ErrTwoFASetupInvalid, "", http.StatusConflict, `{"success":false,"code":"TWOFA_SETUP_INVALID","message":"The two-factor setup has expired or changed. Start setup again."}`, "TWOFA_SETUP_INVALID"},
		{"twofa code", domain.ErrTwoFACodeInvalid, "", http.StatusOK, `{"success":false,"code":"TWOFA_CODE_INVALID","message":"The authenticator code is incorrect."}`, "TWOFA_CODE_INVALID"},
		{"twofa enabled", domain.ErrTwoFAAlreadyEnabled, "", http.StatusOK, `{"success":false,"code":"TWOFA_ALREADY_ENABLED","message":"Two-factor authentication is already enabled."}`, "TWOFA_ALREADY_ENABLED"},
		{"twofa disabled", domain.ErrTwoFANotEnabled, "", http.StatusOK, `{"success":false,"code":"TWOFA_NOT_ENABLED","message":"Two-factor authentication is not enabled."}`, "TWOFA_NOT_ENABLED"},
		{"passkey not found", domain.ErrPasskeyNotFound, "", http.StatusOK, `{"success":false,"code":"PASSKEY_NOT_FOUND","message":"No Passkey is registered."}`, "PASSKEY_NOT_FOUND"},
		{"flow invalid", domain.ErrAuthFlowInvalid, "", http.StatusOK, `{"success":false,"code":"AUTH_FLOW_INVALID","message":"Verification flow expired"}`, "AUTH_FLOW_INVALID"},
		{"flow expired", domain.ErrAuthFlowExpired, "", http.StatusOK, `{"success":false,"code":"AUTH_FLOW_INVALID","message":"Verification flow expired"}`, "AUTH_FLOW_INVALID"},
		{"flow consumed", domain.ErrAuthFlowConsumed, "", http.StatusOK, `{"success":false,"code":"AUTH_FLOW_INVALID","message":"Verification flow expired"}`, "AUTH_FLOW_INVALID"},
		{"session invalid", domain.ErrSessionInvalid, "", http.StatusUnauthorized, `{"success":false,"code":"AUTH_UNAUTHORIZED","message":"Unauthorized"}`, ""},
		{"session inactive", domain.ErrSessionInactive, "", http.StatusUnauthorized, `{"success":false,"code":"AUTH_UNAUTHORIZED","message":"Unauthorized"}`, ""},
		{"token expired", domain.ErrAuthTokenExpired, "", http.StatusUnauthorized, `{"success":false,"code":"AUTH_TOKEN_EXPIRED","message":"Unauthorized"}`, "AUTH_INTERNAL_ERROR"},
		{"record missing", errForeignMissing, "", http.StatusUnauthorized, `{"success":false,"code":"AUTH_UNAUTHORIZED","message":"Unauthorized"}`, "AUTH_INTERNAL_ERROR"},
		{"unknown", errors.New("private database details"), "", http.StatusInternalServerError, `{"success":false,"code":"AUTH_INTERNAL_ERROR","message":"Internal Server Error"}`, "AUTH_INTERNAL_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logged []string
			recorder, c := writeTestError(t, testWriter(&logged).WriteError, tc.err, tc.provider, "")
			assert.Equal(t, tc.status, recorder.Code)
			assert.JSONEq(t, tc.body, recorder.Body.String())
			assert.Equal(t, tc.securityCode, c.GetString("security_error_code"))
			assert.False(t, c.IsAborted())
		})
	}
}

// The expectations repeat the legacy WriteAuthSessionError responses.
func TestWriteSessionErrorKeepsLegacyResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		logged bool
	}{
		{"limit", domain.ErrSessionLimit, http.StatusConflict, "AUTH_SESSION_LIMIT", false},
		{"issuance", domain.ErrSessionIssuanceLimit, http.StatusTooManyRequests, "AUTH_SESSION_ISSUANCE_LIMIT", false},
		{"mismatch", domain.ErrLoginSessionMismatch, http.StatusConflict, "AUTH_SESSION_MISMATCH", false},
		{"refresh race", domain.ErrRefreshRace, http.StatusConflict, "AUTH_REFRESH_RACE", false},
		{"token expired", domain.ErrAuthTokenExpired, http.StatusUnauthorized, "AUTH_TOKEN_EXPIRED", false},
		{"revoked", domain.ErrLoginSessionRevoked, http.StatusUnauthorized, "AUTH_SESSION_REVOKED", false},
		{"refresh invalid", domain.ErrRefreshTokenInvalid, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", false},
		{"token invalid", domain.ErrAuthTokenInvalid, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", false},
		{"record missing", fmt.Errorf("load: %w", errForeignMissing), http.StatusUnauthorized, "AUTH_UNAUTHORIZED", false},
		{"internal", errors.New("redis unavailable"), http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logged []string
			recorder, _ := writeTestError(t, testWriter(&logged).WriteSessionError, tc.err, "", "")
			assert.Equal(t, tc.status, recorder.Code)
			assert.JSONEq(t, fmt.Sprintf(`{"success":false,"code":%q,"message":%q}`, tc.code, http.StatusText(tc.status)), recorder.Body.String())
			if tc.logged {
				assert.Equal(t, []string{"auth session internal error (POST /api/test): redis unavailable"}, logged)
			} else {
				assert.Empty(t, logged)
			}
		})
	}
}

func TestWriteErrorLocalizesPasskeyMessages(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	for lang, want := range map[string]string{"zh-CN": "该用户尚未绑定 Passkey", "ru": "У этого пользователя не привязан Passkey"} {
		var logged []string
		recorder, _ := writeTestError(t, testWriter(&logged).WriteError, domain.ErrPasskeyMissing, "", lang)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, `{"success":false,"message":"`+want+`"}`, recorder.Body.String(), lang)
	}
}

func TestRequireBrowserSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	_, ok := RequireBrowserSession(c, func(*gin.Context) (domain.AuthIdentity, bool) { return domain.AuthIdentity{}, false })
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.JSONEq(t, `{"success":false,"code":"AUTH_SESSION_REQUIRED","message":"a dashboard login session is required"}`, recorder.Body.String())

	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	identity, ok := RequireBrowserSession(c, func(*gin.Context) (domain.AuthIdentity, bool) {
		return domain.AuthIdentity{UserID: 7, SessionID: "sid"}, true
	})
	assert.True(t, ok)
	assert.Equal(t, domain.AuthIdentity{UserID: 7, SessionID: "sid"}, identity)
	assert.Zero(t, recorder.Body.Len())
}
