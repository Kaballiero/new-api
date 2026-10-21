// Package transport holds the incoming HTTP code shared by identity scenarios.
package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
)

// Foreign recognizes errors whose values are owned outside the identity module.
type Foreign struct {
	// EmailTaken reports that the e-mail address belongs to another account.
	EmailTaken func(error) bool
	// ProtocolError reports a WebAuthn protocol error.
	ProtocolError func(error) bool
	// RecordMissing reports a storage "record not found" error.
	RecordMissing func(error) bool
}

// Writer writes identity errors as HTTP responses. app builds it once.
type Writer struct {
	foreign Foreign
	log     func(context.Context, string)
}

// NewWriter returns a writer; log records unexpected session failures with the
// request context.
func NewWriter(foreign Foreign, log func(context.Context, string)) Writer {
	return Writer{foreign: foreign, log: log}
}

// WriteError writes err as the response of a security operation: account
// security, verification, factors, bindings and passkey ceremonies. Session
// and unknown errors are written by WriteSessionError.
func (w Writer) WriteError(c *gin.Context, err error) {
	status := http.StatusOK
	var code, message string
	switch {
	case errors.Is(err, domain.ErrPasskeyIdentityRequired):
		common.ApiErrorI18n(c, i18n.MsgIdentitySecurityVerificationUnsupported)
		return
	case errors.Is(err, domain.ErrPasskeyMissing):
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyNotBound)
		return
	case errors.Is(err, domain.ErrPasskeyRecordInvalid):
		common.ApiErrorI18n(c, i18n.MsgPasskeyCreateFailed)
		return
	case errors.Is(err, domain.ErrPasskeyLoginInvalid):
		common.ApiErrorI18n(c, i18n.MsgPasskeyLoginAbnormal)
		return
	case errors.Is(err, domain.ErrPasskeyAccountDisabled):
		common.ApiErrorI18n(c, i18n.MsgUserDisabled)
		return
	case errors.Is(err, domain.ErrEmailInvalid), errors.Is(err, domain.ErrEmailRestricted):
		code, message = "EMAIL_ADDRESS_REJECTED", err.Error()
	case w.foreign.EmailTaken(err):
		code, message = "EMAIL_ALREADY_TAKEN", "This email address is already in use."
	case errors.Is(err, domain.ErrEmailBindingDelivery):
		code, message = "EMAIL_BINDING_DELIVERY_FAILED", err.Error()
	case errors.Is(err, domain.ErrEmailBindingCodeInvalid):
		code, message = "EMAIL_BINDING_CODE_INVALID", err.Error()
	case errors.Is(err, domain.ErrEmailBindingLocked):
		code, message = "EMAIL_BINDING_LOCKED", err.Error()
	case errors.Is(err, domain.ErrEmailBindingResendWait):
		status = http.StatusTooManyRequests
		code, message = "EMAIL_BINDING_RESEND_WAIT", err.Error()
	case errors.Is(err, common.ErrAccountPasswordLength), errors.Is(err, domain.ErrPasswordSame), errors.Is(err, common.ErrPasswordLegacyLimit):
		code, message = "PASSWORD_POLICY_REJECTED", err.Error()
	case errors.Is(err, domain.ErrCurrentPasswordInvalid):
		code, message = "CURRENT_PASSWORD_INVALID", err.Error()
	case errors.Is(err, domain.ErrPasswordState), errors.Is(err, domain.ErrBindingChanged):
		status = http.StatusConflict
		code, message = "ACCOUNT_SECURITY_STATE_CHANGED", err.Error()
	case errors.Is(err, domain.ErrLastLoginMethod):
		code, message = "LAST_LOGIN_METHOD", err.Error()
	case errors.Is(err, domain.ErrTelegramOAuthNotConfigured):
		code, message = "TELEGRAM_OAUTH_NOT_CONFIGURED", domain.ErrTelegramOAuthNotConfigured.Error()
	case errors.Is(err, domain.ErrTelegramOAuthConflict):
		code, message = "TELEGRAM_OAUTH_CONFLICT", domain.ErrTelegramOAuthConflict.Error()
	case errors.Is(err, domain.ErrTelegramOAuthFailed):
		code, message = "TELEGRAM_OAUTH_FAILED", domain.ErrTelegramOAuthFailed.Error()
	case errors.Is(err, domain.ErrTelegramAccountNotBound):
		code, message = "TELEGRAM_ACCOUNT_NOT_BOUND", domain.ErrTelegramAccountNotBound.Error()
	case errors.Is(err, domain.ErrIdentityClaimed):
		code, message = "ACCOUNT_ALREADY_BOUND", "This external account is already bound."
		if c.Param("provider") == "telegram" {
			code, message = "TELEGRAM_BIND_ALREADY_BOUND", "This Telegram account is already bound."
		}
	case errors.Is(err, domain.ErrVerificationContextInvalid):
		status = http.StatusBadRequest
		code, message = "SECURITY_CONTEXT_INVALID", domain.ErrVerificationContextInvalid.Error()
	case errors.Is(err, domain.ErrVerificationForbidden):
		status = http.StatusForbidden
		code, message = "SECURITY_ACTION_FORBIDDEN", domain.ErrVerificationForbidden.Error()
	case errors.Is(err, domain.ErrVerificationFailed), w.foreign.ProtocolError(err):
		code, message = "SECURITY_VERIFICATION_FAILED", domain.ErrVerificationFailed.Error()
	case errors.Is(err, domain.ErrVerificationLocked):
		code, message = "SECURITY_VERIFICATION_LOCKED", domain.ErrVerificationLocked.Error()
	case errors.Is(err, domain.ErrVerificationUnavailable):
		code, message = "SECURITY_METHOD_UNAVAILABLE", domain.ErrVerificationUnavailable.Error()
	case errors.Is(err, domain.ErrVerificationFlowRequired):
		status = http.StatusBadRequest
		code, message = "SECURITY_VERIFICATION_FLOW_REQUIRED", domain.ErrVerificationFlowRequired.Error()
	case errors.Is(err, domain.ErrProofMethod):
		code, message = "SECURITY_PROOF_METHOD_MISMATCH", "This verification method is not allowed for this action."
	case errors.Is(err, domain.ErrProofScope):
		code, message = "SECURITY_PROOF_SCOPE_MISMATCH", "Verification does not match this action."
	case errors.Is(err, domain.ErrOAuthAccountMismatch):
		code, message = "OAUTH_ACCOUNT_MISMATCH", domain.ErrOAuthAccountMismatch.Error()
	case errors.Is(err, domain.ErrTwoFASetupInvalid):
		status = http.StatusConflict
		code, message = "TWOFA_SETUP_INVALID", domain.ErrTwoFASetupInvalid.Error()
	case errors.Is(err, domain.ErrTwoFACodeInvalid):
		code, message = "TWOFA_CODE_INVALID", domain.ErrTwoFACodeInvalid.Error()
	case errors.Is(err, domain.ErrTwoFAAlreadyEnabled):
		code, message = "TWOFA_ALREADY_ENABLED", "Two-factor authentication is already enabled."
	case errors.Is(err, domain.ErrTwoFANotEnabled):
		code, message = "TWOFA_NOT_ENABLED", "Two-factor authentication is not enabled."
	case errors.Is(err, domain.ErrPasskeyNotFound):
		code, message = "PASSKEY_NOT_FOUND", "No Passkey is registered."
	case errors.Is(err, domain.ErrAuthFlowInvalid), errors.Is(err, domain.ErrAuthFlowExpired), errors.Is(err, domain.ErrAuthFlowConsumed):
		code, message = "AUTH_FLOW_INVALID", "Verification flow expired"
	case errors.Is(err, domain.ErrSessionInvalid), errors.Is(err, domain.ErrSessionInactive):
		w.WriteSessionError(c, domain.ErrAuthTokenInvalid)
		return
	default:
		c.Set("security_error_code", "AUTH_INTERNAL_ERROR")
		w.WriteSessionError(c, err)
		return
	}
	c.Set("security_error_code", code)
	c.JSON(status, gin.H{"success": false, "code": code, "message": message})
}

// WriteSessionError writes err as the response of a login session operation:
// the status and code of the session error and the status text as message.
func (w Writer) WriteSessionError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "AUTH_INTERNAL_ERROR"
	switch {
	case errors.Is(err, domain.ErrSessionLimit):
		status, code = http.StatusConflict, "AUTH_SESSION_LIMIT"
	case errors.Is(err, domain.ErrSessionIssuanceLimit):
		status, code = http.StatusTooManyRequests, "AUTH_SESSION_ISSUANCE_LIMIT"
	case errors.Is(err, domain.ErrLoginSessionMismatch):
		status, code = http.StatusConflict, "AUTH_SESSION_MISMATCH"
	case errors.Is(err, domain.ErrRefreshRace):
		status, code = http.StatusConflict, "AUTH_REFRESH_RACE"
	case errors.Is(err, domain.ErrAuthTokenExpired):
		status, code = http.StatusUnauthorized, "AUTH_TOKEN_EXPIRED"
	case errors.Is(err, domain.ErrLoginSessionRevoked):
		status, code = http.StatusUnauthorized, "AUTH_SESSION_REVOKED"
	case errors.Is(err, domain.ErrRefreshTokenInvalid), errors.Is(err, domain.ErrAuthTokenInvalid):
		status, code = http.StatusUnauthorized, "AUTH_UNAUTHORIZED"
	}
	if w.foreign.RecordMissing(err) {
		status, code = http.StatusUnauthorized, "AUTH_UNAUTHORIZED"
	}
	if status == http.StatusInternalServerError {
		// The response body only carries the generic AUTH_INTERNAL_ERROR
		// code; without this log the underlying Redis/database/session
		// failure is indistinguishable from the client side.
		w.log(c.Request.Context(), fmt.Sprintf("auth session internal error (%s %s): %v", c.Request.Method, c.Request.URL.Path, err))
	}
	c.JSON(status, gin.H{"success": false, "code": code, "message": http.StatusText(status)})
}
