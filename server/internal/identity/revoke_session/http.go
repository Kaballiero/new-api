package revoke_session

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	revoker     Revoker
	identity    func(*gin.Context) (domain.AuthIdentity, bool)
	cookieName  string
	refreshSID  func(string) (string, bool)
	clearCookie func(*gin.Context)
	writeError  func(*gin.Context, error)
}

// NewHTTP builds the handler; the refresh cookie is cleared when it belongs to
// the revoked session.
func NewHTTP(revoker Revoker, identity func(*gin.Context) (domain.AuthIdentity, bool), cookieName string, refreshSID func(string) (string, bool), clearCookie func(*gin.Context), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{revoker: revoker, identity: identity, cookieName: cookieName, refreshSID: refreshSID, clearCookie: clearCookie, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := transport.RequireBrowserSession(c, h.identity)
	if !ok {
		return
	}
	sid, err := NewUsecase(h.revoker).Execute(Input{UserID: identity.UserID, SID: c.Param("sid")})
	switch {
	case errors.Is(err, domain.ErrSessionIDRequired):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "AUTH_SESSION_ID_REQUIRED", "message": "session id is required"})
		return
	case errors.Is(err, domain.ErrSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "code": "AUTH_SESSION_NOT_FOUND", "message": "session not found"})
		return
	case err != nil:
		h.writeError(c, err)
		return
	}
	if rawRefreshToken, cookieErr := c.Cookie(h.cookieName); cookieErr == nil {
		cookieSID, ok := h.refreshSID(rawRefreshToken)
		if ok && cookieSID == sid {
			h.clearCookie(c)
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"revoked_sid": sid, "current": sid == identity.SessionID}})
}
