package confirm_email_binding

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

type Completion interface {
	Confirmation
	Succeeded() bool
	NotificationWarning() bool
}
type HTTP struct {
	identity   func(*gin.Context) (domain.AuthIdentity, bool)
	factory    func(domain.AuthIdentity, Request) Completion
	writeError func(*gin.Context, error)
	audit      func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(identity func(*gin.Context) (domain.AuthIdentity, bool), factory func(domain.AuthIdentity, Request) Completion, writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{identity: identity, factory: factory, writeError: writeError, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		h.writeError(c, domain.ErrAuthTokenInvalid)
		return
	}
	var op Completion
	defer func() {
		succeeded, warning := false, false
		if op != nil {
			succeeded, warning = op.Succeeded(), op.NotificationWarning()
		}
		h.audit(c, identity.UserID, "user.binding_bind", map[string]any{"provider": "email", "success": succeeded, "notification_failed": warning})
	}()
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.FlowToken == "" {
		h.writeError(c, domain.ErrAuthFlowInvalid)
		return
	}
	op = h.factory(identity, request)
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"notification_warning": op.NotificationWarning()}})
}
