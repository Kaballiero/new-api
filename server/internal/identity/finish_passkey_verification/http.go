package finish_passkey_verification

import (
	"github.com/QuantumNous/new-api/common"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"net/http"
)

type Completion interface {
	Operation
	Proof() *dto.SecurityProof
}
type HTTP struct {
	factory func(c *gin.Context, user *model.User, request Request, parsed *protocol.ParsedCredentialAssertionData) Completion
	audit   func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(factory func(c *gin.Context, user *model.User, request Request, parsed *protocol.ParsedCredentialAssertionData) Completion, audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{factory: factory, audit: audit}
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
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.FlowToken == "" || len(request.Credential) == 0 {
		common.ApiErrorMsg(c, "无效的 Passkey 验证请求")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	op := h.factory(c, user, request, parsed)
	failure, err := NewUsecase(op).Execute()
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	if identityhttp.WritePasskeyFailure(c, failure) {
		return
	}
	proof := op.Proof()
	h.audit(c, user.Id, "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope})
	common.ApiSuccess(c, proof)
}
