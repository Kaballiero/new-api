package delete_passkey

import (
	"github.com/QuantumNous/new-api/common"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

type Completion interface {
	Operation
	Bundle() *dto.AuthBundle
}
type HTTP struct {
	factory func(Input) Completion
	audit   func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(factory func(Input) Completion, audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{factory: factory, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	user, err := identityhttp.GetAuthenticatedUser(c)
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePasskeyDelete}) == nil {
		return
	}
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		common.ApiErrorMsg(c, "当前认证方式不支持安全验证")
		return
	}
	op := h.factory(Input{Identity: identity})
	if err := NewUsecase(op).Execute(); err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	h.audit(c, user.Id, "user.passkey_delete", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Passkey 已解绑", "data": identityhttp.AuthRotationData(op.Bundle())})
}
