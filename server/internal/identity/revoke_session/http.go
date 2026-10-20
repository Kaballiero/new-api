package revoke_session

import (
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ revoker Revoker }

func NewHTTP(revoker Revoker) *HTTP { return &HTTP{revoker: revoker} }
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := identityhttp.RequireBrowserSession(c)
	if !ok {
		return
	}
	result, err := NewUsecase(h.revoker).Execute(Input{UserID: identity.UserID, SID: c.Param("sid")})
	if err != nil {
		identityhttp.WriteAuthSessionError(c, err)
		return
	}
	sid := result.SID
	if result.Invalid {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "AUTH_SESSION_ID_REQUIRED", "message": "session id is required"})
		return
	}
	if !result.Revoked {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "code": "AUTH_SESSION_NOT_FOUND", "message": "session not found"})
		return
	}
	if rawRefreshToken, cookieErr := c.Cookie(service.RefreshCookieName); cookieErr == nil {
		cookieSID, ok := service.RefreshTokenSID(rawRefreshToken)
		if ok && cookieSID == sid {
			service.ClearRefreshCookie(c)
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"revoked_sid": sid, "current": sid == identity.SessionID}})
}
