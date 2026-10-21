package transport

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
)

// RequireBrowserSession returns the dashboard session identity read by
// identity, or writes 403 AUTH_SESSION_REQUIRED when the request is not
// authenticated by a browser session.
func RequireBrowserSession(c *gin.Context, identity func(*gin.Context) (domain.AuthIdentity, bool)) (domain.AuthIdentity, bool) {
	current, ok := identity(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"code":    "AUTH_SESSION_REQUIRED",
			"message": "a dashboard login session is required",
		})
		return domain.AuthIdentity{}, false
	}
	return current, true
}

// RotationData is the response data of a rotated session.
func RotationData(bundle *dto.AuthBundle) gin.H {
	return gin.H{
		"access_token":      bundle.AccessToken,
		"token_type":        bundle.TokenType,
		"access_expires_at": bundle.AccessExpiresAt,
		"session":           bundle.Session,
	}
}
