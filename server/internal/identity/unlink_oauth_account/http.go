package unlink_oauth_account

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type Completion interface {
	Confirmation
	Succeeded() bool
	NotificationWarning() bool
}
type HTTP struct {
	identity   func(*gin.Context) (domain.AuthIdentity, bool)
	invalid    func() error
	authorize  func(*gin.Context, []byte) bool
	factory    func(Input) Completion
	writeError func(*gin.Context, error)
	audit      func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(identity func(*gin.Context) (domain.AuthIdentity, bool), invalid func() error, authorize func(*gin.Context, []byte) bool, factory func(Input) Completion, writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{identity: identity, invalid: invalid, authorize: authorize, factory: factory, writeError: writeError, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		h.writeError(c, h.invalid())
		return
	}
	providerID, err := strconv.Atoi(c.Param("provider_id"))
	if err != nil || providerID <= 0 {
		common.ApiErrorMsg(c, "无效的提供商 ID")
		return
	}
	var op Completion
	defer func() {
		succeeded, warning := false, false
		if op != nil {
			succeeded, warning = op.Succeeded(), op.NotificationWarning()
		}
		h.audit(c, identity.UserID, "user.binding_unbind", map[string]any{"provider_id": providerID, "success": succeeded, "notification_failed": warning})
	}()
	context, err := common.Marshal(domain.AccountUnbindingContext{ProviderID: providerID})
	if err != nil {
		h.writeError(c, err)
		return
	}
	if !h.authorize(c, context) {
		return
	}
	op = h.factory(Input{Identity: identity, ProviderID: providerID})
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "解绑成功", "data": gin.H{"notification_warning": op.NotificationWarning()}})
}
