package begin_login_passkey

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/dto"

	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Challenge() dto.PasskeyChallenge
}
type HTTP struct {
	factory    func(*gin.Context, Request) Completion
	writeError func(*gin.Context, error)
}

func NewHTTP(factory func(*gin.Context, Request) Completion, writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{factory: factory, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	var request Request
	if common.DecodeJson(c.Request.Body, &request) != nil || request.FlowToken == "" {
		common.ApiErrorI18n(c, i18n.MsgIdentityParamsError)
		return
	}
	op := h.factory(c, request)
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	result := op.Challenge()
	common.ApiSuccess(c, gin.H{"flow_token": result.FlowToken, "expires_at": result.ExpiresAt, "options": result.Options})
}
