package finish_login_passkey

import (
	"github.com/QuantumNous/new-api/common"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Bundle() *dto.AuthBundle
}
type HTTP struct {
	factory  func(*gin.Context, Request) Completion
	complete func(*gin.Context, *dto.AuthBundle, string)
}

func NewHTTP(factory func(*gin.Context, Request) Completion, complete func(*gin.Context, *dto.AuthBundle, string)) *HTTP {
	return &HTTP{factory: factory, complete: complete}
}
func (h *HTTP) Handle(c *gin.Context) {
	var request Request
	if common.DecodeJson(c.Request.Body, &request) != nil || request.FlowToken == "" || request.PasskeyFlowToken == "" || len(request.Credential) == 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	op := h.factory(c, request)
	if err := NewUsecase(op).Execute(); err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	h.complete(c, op.Bundle(), "passkey")
}
