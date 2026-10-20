package generate_token

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	store       Store
	authorize   func(*gin.Context) bool
	generate    func() (string, error)
	fingerprint func(string) string
	audit       func(*gin.Context, int, string)
	writeError  func(*gin.Context, error)
}

func NewHTTP(store Store, authorize func(*gin.Context) bool, generate func() (string, error), fingerprint func(string) string, audit func(*gin.Context, int, string), writeError func(*gin.Context, error)) HTTP {
	return HTTP{store: store, authorize: authorize, generate: generate, fingerprint: fingerprint, audit: audit, writeError: writeError}
}
func (h HTTP) Handle(c *gin.Context) {
	if !h.authorize(c) {
		return
	}
	id := c.GetInt("id")
	output, err := NewUsecase(h.store, h.generate, h.fingerprint, func(id int, ref string) { h.audit(c, id, ref) }).Execute(Input{UserID: id})
	if err != nil {
		h.writeError(c, err)
		return
	}
	if output.Duplicate {
		common.ApiErrorI18n(c, i18n.MsgUuidDuplicate)
		return
	}
	c.JSON(http.StatusOK, Response{Success: true, Message: "", Data: output.Token})
}
