package begin_passkey_verification

import (
	"github.com/QuantumNous/new-api/common"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"net/http"
)

type Completion interface {
	Operation
	Challenge() dto.PasskeyChallenge
}
type HTTP struct {
	factory func(c *gin.Context, user *model.User, request Request) Completion
}

func NewHTTP(factory func(c *gin.Context, user *model.User, request Request) Completion) *HTTP {
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
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "无效的 Passkey 验证请求")
		return
	}
	op := h.factory(c, user, request)
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
