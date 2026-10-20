package begin_passkey_registration

import (
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"net/http"
)

type Completion interface {
	Operation
	Challenge() dto.PasskeyChallenge
}
type HTTP struct {
	factory func(c *gin.Context, user *model.User, authorization *model.AuthFlowAuthorization) Completion
}

func NewHTTP(factory func(c *gin.Context, user *model.User, authorization *model.AuthFlowAuthorization) Completion) *HTTP {
	return &HTTP{factory: factory}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "管理员未启用 Passkey 登录"})
		return
	}
	user, err := identityhttp.GetAuthenticatedUser(c)
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePasskeyRegister})
	if authorization == nil {
		return
	}
	op := h.factory(c, user, authorization)
	failure, err := NewUsecase(op).Execute()
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	if identityhttp.WritePasskeyFailure(c, failure) {
		return
	}
	result := op.Challenge()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"options": result.Options, "flow_token": result.FlowToken, "expires_at": result.ExpiresAt}})
}
