package finish_passkey_login

import (
	"github.com/QuantumNous/new-api/common"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"net/http"
)

type Completion interface {
	Operation
	LoginAccount() *model.User
}
type HTTP struct {
	factory  func(c *gin.Context, request Request, parsed *protocol.ParsedCredentialAssertionData) Completion
	complete func(*gin.Context, *model.User, int64)
}

func NewHTTP(factory func(c *gin.Context, request Request, parsed *protocol.ParsedCredentialAssertionData) Completion, complete func(*gin.Context, *model.User, int64)) *HTTP {
	return &HTTP{factory: factory, complete: complete}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "管理员未启用 Passkey 登录"})
		return
	}
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.FlowToken == "" || len(request.Credential) == 0 {
		common.ApiErrorMsg(c, "无效的 Passkey 验证请求")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	op := h.factory(c, request, parsed)
	failure, err := NewUsecase(op).Execute()
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	if identityhttp.WritePasskeyFailure(c, failure) {
		return
	}
	user := op.LoginAccount()
	c.Set("login_verification_method", "passkey")
	h.complete(c, user, user.AuthVersion)
}
