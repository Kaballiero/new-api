package finish_login_passkey

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/dto"

	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Bundle() *dto.AuthBundle
}
type HTTP struct {
	factory    func(*gin.Context, Request) Completion
	complete   func(*gin.Context, *dto.AuthBundle, string)
	writeError func(*gin.Context, error)
}

func NewHTTP(factory func(*gin.Context, Request) Completion, complete func(*gin.Context, *dto.AuthBundle, string), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{factory: factory, complete: complete, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	var request Request
	if common.DecodeJson(c.Request.Body, &request) != nil || request.FlowToken == "" || request.PasskeyFlowToken == "" || len(request.Credential) == 0 {
		common.ApiErrorI18n(c, i18n.MsgIdentityParamsError)
		return
	}
	op := h.factory(c, request)
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	h.complete(c, op.Bundle(), "passkey")
}
