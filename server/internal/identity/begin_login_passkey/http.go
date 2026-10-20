package begin_login_passkey

import (
	"github.com/QuantumNous/new-api/common"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Challenge() dto.PasskeyChallenge
}
type HTTP struct {
	factory func(*gin.Context, Request) Completion
}

func NewHTTP(factory func(*gin.Context, Request) Completion) *HTTP { return &HTTP{factory: factory} }
func (h *HTTP) Handle(c *gin.Context) {
	var request Request
	if common.DecodeJson(c.Request.Body, &request) != nil || request.FlowToken == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	op := h.factory(c, request)
	if err := NewUsecase(op).Execute(); err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	result := op.Challenge()
	common.ApiSuccess(c, gin.H{"flow_token": result.FlowToken, "expires_at": result.ExpiresAt, "options": result.Options})
}
