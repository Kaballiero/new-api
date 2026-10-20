package start_oauth_flow

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/gin-gonic/gin"
	"net/http"
)

type Completion interface {
	Operation
	BindingAudit() (bool, int, bool)
	Response() map[string]any
}
type HTTP struct {
	factory func(*gin.Context, *Request) Completion
	audit   func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(factory func(*gin.Context, *Request) Completion, audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{factory: factory, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	op := h.factory(c, &request)
	defer func() {
		required, id, succeeded := op.BindingAudit()
		if required {
			h.audit(c, id, "user.binding_start", map[string]any{"provider": request.Provider, "success": succeeded})
		}
	}()
	result := NewUsecase(op).Execute(&request)
	if result.Cause != nil {
		identityhttp.WriteSecurityOperationError(c, result.Cause)
		return
	}
	switch result.Issue {
	case "invalid":
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	case "session":
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "绑定操作需要登录"})
		return
	case "stopped":
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": op.Response()})
}
