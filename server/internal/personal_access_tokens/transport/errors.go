// Package transport holds the incoming HTTP code shared by personal access
// token scenarios.
package transport

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"
	"github.com/gin-gonic/gin"
)

// WriteError writes err as the HTTP error response of a personal access token
// scenario. PAT management is a security operation, so every other failure
// (domain.Internal storage errors included) is written by writeSecurityError,
// the security operation writer shared with the identity flows: it keeps the
// AUTH_UNAUTHORIZED / AUTH_INTERNAL_ERROR envelope and the security audit code.
func WriteError(c *gin.Context, err error, writeSecurityError func(*gin.Context, error)) {
	switch {
	case errors.Is(err, domain.ErrTokenDuplicate):
		common.ApiErrorI18n(c, i18n.MsgUuidDuplicate)
	default:
		writeSecurityError(c, err)
	}
}
