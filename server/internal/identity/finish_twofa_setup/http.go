package finish_twofa_setup

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	EnableOperation
	Bundle() *dto.AuthBundle
}
type HTTP struct {
	identity   func(*gin.Context) (domain.AuthIdentity, bool)
	invalid    func() error
	operation  func(domain.AuthIdentity, Verify2FARequest) Completion
	writeError func(*gin.Context, error)
	audit      func(*gin.Context, int, string, map[string]any)
	rotation   func(*dto.AuthBundle) gin.H
}

func NewHTTP(identity func(*gin.Context) (domain.AuthIdentity, bool), invalid func() error, operation func(domain.AuthIdentity, Verify2FARequest) Completion, writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any), rotation func(*dto.AuthBundle) gin.H) *HTTP {
	return &HTTP{identity: identity, invalid: invalid, operation: operation, writeError: writeError, audit: audit, rotation: rotation}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		h.writeError(c, h.invalid())
		return
	}
	var req Verify2FARequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	op := h.operation(identity, req)
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	h.audit(c, identity.UserID, "user.2fa_enable", nil)
	common.ApiSuccess(c, h.rotation(op.Bundle()))
}
