package logout

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

type HTTP struct {
	operation  Operation
	cookieName string
	clear      func(*gin.Context)
	writeError func(*gin.Context, error)
}

func NewHTTP(op Operation, cookieName string, clear func(*gin.Context), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{operation: op, cookieName: cookieName, clear: clear, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	raw, cookieErr := c.Cookie(h.cookieName)
	parts := strings.Fields(c.GetHeader("Authorization"))
	access := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
		access = parts[1]
	}
	result, err := NewUsecase(h.operation).Execute(Input{ExpectedSID: c.GetHeader("X-Auth-Session"), RefreshToken: raw, CookiePresent: cookieErr == nil, AccessToken: access})
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result.ClearCookie {
		h.clear(c)
	}
	if result.Detailed {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"revoked_sid": result.RevokedSID, "cookie_cleared": result.ClearCookie}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
