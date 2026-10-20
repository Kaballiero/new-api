package bind_wechat

import (
	"github.com/QuantumNous/new-api/common"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

type Completion interface {
	Operation
	Succeeded() bool
	NotificationWarning() bool
}
type HTTP struct {
	factory func(Input) Completion
	audit   func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(factory func(Input) Completion, audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{factory: factory, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		identityhttp.WriteSecurityOperationError(c, service.ErrAuthTokenInvalid)
		return
	}
	var op Completion
	defer func() {
		succeeded, warning := false, false
		if op != nil {
			succeeded, warning = op.Succeeded(), op.NotificationWarning()
		}
		h.audit(c, identity.UserID, "user.binding_bind", map[string]any{"provider": "wechat", "success": succeeded, "notification_failed": warning})
	}()
	if !common.WeChatAuthEnabled {
		c.JSON(http.StatusOK, gin.H{"message": "管理员未开启通过微信登录以及注册", "success": false})
		return
	}
	var req Request
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的请求"})
		return
	}
	code := strings.TrimSpace(req.Code)
	context, err := common.Marshal(domain.AccountBindingContext{Provider: "wechat", Code: code})
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: context}) == nil {
		return
	}
	input := Input{Identity: identity, Code: code}
	op = h.factory(input)
	result := NewUsecase(op).Execute(input)
	if result.Cause != nil {
		if result.Issue == "provider" {
			c.JSON(http.StatusOK, gin.H{"message": result.Cause.Error(), "success": false})
		} else {
			identityhttp.WriteSecurityOperationError(c, result.Cause)
		}
		return
	}
	if result.Issue == "taken" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "该微信账号已被绑定"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"notification_warning": op.NotificationWarning()}})
}
