package begin_passkey_login

import (
	"net/http"

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
	enabled    func() bool
	factory    func(*gin.Context) Completion
	writeError func(*gin.Context, error)
}

func NewHTTP(enabled func() bool, factory func(*gin.Context) Completion, writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{enabled: enabled, factory: factory, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyDisabled)
		return
	}
	op := h.factory(c)
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	result := op.Challenge()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"options": result.Options, "flow_token": result.FlowToken, "expires_at": result.ExpiresAt}})
}
