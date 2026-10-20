package delete_account

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	operation Operation
	audit     func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(op Operation, audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{operation: op, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	succeeded := false
	defer func() { h.audit(c, c.GetInt("id"), "user.account_delete", map[string]any{"success": succeeded}) }()
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountDelete}) == nil {
		return
	}
	identity, _ := middleware.GetSessionAuthIdentity(c)
	result := NewUsecase(h.operation).Execute(Input{Identity: identity})
	if result.RootDenied {
		common.ApiErrorI18n(c, i18n.MsgUserCannotDeleteRootUser)
		return
	}
	if result.Cause != nil {
		identityhttp.WriteSecurityOperationError(c, result.Cause)
		return
	}
	succeeded = true
	service.ClearRefreshCookie(c)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{}})
}
