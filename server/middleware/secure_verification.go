package middleware

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"

	identitycore "github.com/QuantumNous/new-api/internal/app/identitycore"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
)

// SecureVerificationRequired protects channel key disclosure. Other sensitive
// operations validate their narrower proof scopes in their controller.
func SecureVerificationRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil || channelID <= 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "SECURITY_CONTEXT_INVALID", "message": identitydomain.ErrVerificationContextInvalid.Error()})
			return
		}
		context, err := common.Marshal(identitydomain.ChannelKeyReadContext{ChannelID: channelID})
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if RequireSecurityProof(c, identitydomain.VerificationOperation{Scope: identitydomain.VerificationScopeChannelKeyRead, Context: context}) == nil {
			return
		}
		c.Set("secure_verified", true)
		c.Next()
	}
}

// RequireSecurityProof validates a proof against the authenticated dashboard
// session and writes the shared proof error contract on failure.
func RequireSecurityProof(c *gin.Context, operation identitydomain.VerificationOperation) *model.AuthFlowAuthorization {
	identity, ok := GetSessionAuthIdentity(c)
	if !ok {
		securityProofError(c, "SECURITY_PROOF_INVALID", common.TranslateMessage(c, i18n.MsgSecurityProofInvalid))
		return nil
	}
	raw := strings.TrimSpace(c.GetHeader("X-Security-Proof"))
	if raw == "" {
		securityProofError(c, "SECURITY_PROOF_REQUIRED", common.TranslateMessage(c, i18n.MsgSecurityProofRequired))
		return nil
	}
	authorization, err := identitycore.ConsumeOperationProof(raw, identity, operation)
	if err != nil {
		switch {
		case errors.Is(err, identitydomain.ErrAuthTokenExpired):
			securityProofError(c, "SECURITY_PROOF_EXPIRED", common.TranslateMessage(c, i18n.MsgSecurityProofExpired))
		case errors.Is(err, identitydomain.ErrProofScope):
			securityProofError(c, "SECURITY_PROOF_SCOPE_MISMATCH", common.TranslateMessage(c, i18n.MsgSecurityProofScopeMismatch))
		case errors.Is(err, identitydomain.ErrVerificationUnavailable):
			securityProofError(c, "SECURITY_METHOD_UNAVAILABLE", identitydomain.ErrVerificationUnavailable.Error())
		case errors.Is(err, identitydomain.ErrProofMethod):
			securityProofError(c, "SECURITY_PROOF_METHOD_MISMATCH", common.TranslateMessage(c, i18n.MsgSecurityProofMethodMismatch))
		case errors.Is(err, identitydomain.ErrProofConsumed):
			securityProofError(c, "SECURITY_PROOF_CONSUMED", "This verification has already been used. Please verify again.")
		case errors.Is(err, identitydomain.ErrProofContext):
			securityProofError(c, "SECURITY_PROOF_CONTEXT_MISMATCH", "Verification does not match this action's details. Please verify again.")
		case errors.Is(err, identitydomain.ErrVerificationForbidden):
			securityProofError(c, "SECURITY_ACTION_FORBIDDEN", identitydomain.ErrVerificationForbidden.Error())
		case errors.Is(err, identitydomain.ErrAuthTokenInvalid), errors.Is(err, identitydomain.ErrLoginSessionInvalid), errors.Is(err, identitydomain.ErrLoginSessionRevoked), errors.Is(err, model.ErrUserSessionInactive):
			securityProofError(c, "SECURITY_PROOF_INVALID", common.TranslateMessage(c, i18n.MsgSecurityProofInvalid))
		default:
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "code": "AUTH_INTERNAL_ERROR", "message": "Please try again later."})
		}
		return nil
	}
	return authorization
}

func securityProofError(c *gin.Context, code, message string) {
	c.Set("security_error_code", code)
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": message,
		"code":    code,
	})
	c.Abort()
}
