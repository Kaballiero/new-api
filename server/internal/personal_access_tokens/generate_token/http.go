package generate_token

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/personal_access_tokens/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store              Store
	authorize          func(*gin.Context) bool
	generate           func() (string, error)
	fingerprint        func(string) string
	audit              func(*gin.Context, int, string)
	writeSecurityError func(*gin.Context, error)
}

func NewHTTP(store Store, authorize func(*gin.Context) bool, generate func() (string, error), fingerprint func(string) string, audit func(*gin.Context, int, string), writeSecurityError func(*gin.Context, error)) HTTP {
	return HTTP{store: store, authorize: authorize, generate: generate, fingerprint: fingerprint, audit: audit, writeSecurityError: writeSecurityError}
}
func (h HTTP) Handle(c *gin.Context) {
	if !h.authorize(c) {
		return
	}
	id := c.GetInt("id")
	output, err := NewUsecase(h.store, h.generate, h.fingerprint, func(id int, ref string) { h.audit(c, id, ref) }).Execute(Input{UserID: id})
	if err != nil {
		transport.WriteError(c, err, h.writeSecurityError)
		return
	}
	c.JSON(http.StatusOK, Response{Success: true, Message: "", Data: output.Token})
}
