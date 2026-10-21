package begin_passkey_verification

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Challenge() dto.PasskeyChallenge
}
type HTTP struct {
	enabled    func() bool
	user       func(*gin.Context) (*model.User, error)
	factory    func(c *gin.Context, user *model.User, request Request) Completion
	writeError func(*gin.Context, error)
}

func NewHTTP(enabled func() bool, user func(*gin.Context) (*model.User, error), factory func(c *gin.Context, user *model.User, request Request) Completion, writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{enabled: enabled, user: user, factory: factory, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyDisabled)
		return
	}
	user, err := h.user(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyRequestInvalid)
		return
	}
	op := h.factory(c, user, request)
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	result := op.Challenge()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"options": result.Options, "flow_token": result.FlowToken, "expires_at": result.ExpiresAt}})
}
