package refresh_session

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	refresh          func(string, string, string, string) (Output, error)
	cookieName       string
	clear            func(*gin.Context)
	write            func(*gin.Context, string)
	writeError       func(*gin.Context, error)
	invalid, revoked func() error
}

func NewHTTP(refresh func(string, string, string, string) (Output, error), cookieName string, clear func(*gin.Context), write func(*gin.Context, string), writeError func(*gin.Context, error), invalid, revoked func() error) *HTTP {
	return &HTTP{refresh: refresh, cookieName: cookieName, clear: clear, write: write, writeError: writeError, invalid: invalid, revoked: revoked}
}
func (h *HTTP) Handle(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	raw, err := c.Cookie(h.cookieName)
	if err != nil || raw == "" {
		h.clear(c)
		h.writeError(c, h.invalid())
		return
	}
	result, err := h.refresh(raw, c.GetHeader("X-Auth-Session"), c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		if errors.Is(err, h.invalid()) || errors.Is(err, h.revoked()) {
			h.clear(c)
		}
		h.writeError(c, err)
		return
	}
	h.write(c, result.Bundle.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"access_token": result.Bundle.AccessToken, "token_type": result.Bundle.TokenType, "access_expires_at": result.Bundle.AccessExpiresAt, "user": result.Profile(), "session": result.Bundle.Session}})
}
