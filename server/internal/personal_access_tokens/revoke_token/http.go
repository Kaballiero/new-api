package revoke_token

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	store       Store
	authorize   func(*gin.Context) bool
	fingerprint func(string) string
	audit       func(*gin.Context, int, string)
	writeError  func(*gin.Context, error)
}

func NewHTTP(store Store, authorize func(*gin.Context) bool, fingerprint func(string) string, audit func(*gin.Context, int, string), writeError func(*gin.Context, error)) HTTP {
	return HTTP{store: store, authorize: authorize, fingerprint: fingerprint, audit: audit, writeError: writeError}
}
func (h HTTP) Handle(c *gin.Context) {
	if !h.authorize(c) {
		return
	}
	_, err := NewUsecase(h.store, h.fingerprint, func(ref string) { h.audit(c, c.GetInt("id"), ref) }).Execute(Input{UserID: c.GetInt("id")})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, Response{Success: true, Message: "", Data: nil})
}
