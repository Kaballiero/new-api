package revoke_token

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/personal_access_tokens/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store              Store
	authorize          func(*gin.Context) bool
	fingerprint        func(string) string
	audit              func(*gin.Context, int, string)
	writeSecurityError func(*gin.Context, error)
}

func NewHTTP(store Store, authorize func(*gin.Context) bool, fingerprint func(string) string, audit func(*gin.Context, int, string), writeSecurityError func(*gin.Context, error)) HTTP {
	return HTTP{store: store, authorize: authorize, fingerprint: fingerprint, audit: audit, writeSecurityError: writeSecurityError}
}
func (h HTTP) Handle(c *gin.Context) {
	if !h.authorize(c) {
		return
	}
	_, err := NewUsecase(h.store, h.fingerprint, func(ref string) { h.audit(c, c.GetInt("id"), ref) }).Execute(Input{UserID: c.GetInt("id")})
	if err != nil {
		transport.WriteError(c, err, h.writeSecurityError)
		return
	}
	c.JSON(http.StatusOK, Response{Success: true, Message: "", Data: nil})
}
