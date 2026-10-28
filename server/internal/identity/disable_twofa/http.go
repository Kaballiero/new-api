package disable_twofa

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"

	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Bundle() *dto.AuthBundle
}
type HTTP struct {
	authorize  func(*gin.Context) (domain.AuthIdentity, bool)
	operation  func(Input) Completion
	writeError func(*gin.Context, error)
	audit      func(*gin.Context, int, string, map[string]any)
	rotation   func(*dto.AuthBundle) gin.H
}

func NewHTTP(authorize func(*gin.Context) (domain.AuthIdentity, bool), operation func(Input) Completion, writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any), rotation func(*dto.AuthBundle) gin.H) *HTTP {
	return &HTTP{authorize: authorize, operation: operation, writeError: writeError, audit: audit, rotation: rotation}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.authorize(c)
	if !ok {
		return
	}
	op := h.operation(Input{Identity: identity})
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	h.audit(c, identity.UserID, "user.2fa_disable_self", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": common.TranslateMessage(c, i18n.MsgIdentityTwoFADisabled), "data": h.rotation(op.Bundle())})
}
