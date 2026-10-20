package start_email_binding

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	identity                func(*gin.Context) (domain.AuthIdentity, bool)
	invalid, contextInvalid func() error
	validate                func(string) (string, error)
	authorize               func(*gin.Context, []byte) *domain.FlowAuthorization
	start                   func(domain.AuthIdentity, *domain.FlowAuthorization, string) (*Output, error)
	writeError              func(*gin.Context, error)
	audit                   func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(identity func(*gin.Context) (domain.AuthIdentity, bool), invalid, contextInvalid func() error, validate func(string) (string, error), authorize func(*gin.Context, []byte) *domain.FlowAuthorization, start func(domain.AuthIdentity, *domain.FlowAuthorization, string) (*Output, error), writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{identity: identity, invalid: invalid, contextInvalid: contextInvalid, validate: validate, authorize: authorize, start: start, writeError: writeError, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		h.writeError(c, h.invalid())
		return
	}
	succeeded, warning := false, false
	defer func() {
		h.audit(c, identity.UserID, "user.binding_start", map[string]any{"provider": "email", "success": succeeded, "notification_failed": warning})
	}()
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		h.writeError(c, h.contextInvalid())
		return
	}
	email, err := h.validate(request.Email)
	if err != nil {
		h.writeError(c, err)
		return
	}
	context, err := common.Marshal(domain.AccountBindingContext{Provider: "email", Email: email})
	if err != nil {
		h.writeError(c, err)
		return
	}
	authorization := h.authorize(c, context)
	if authorization == nil {
		return
	}
	data, err := h.start(identity, authorization, email)
	if err != nil {
		h.writeError(c, err)
		return
	}
	succeeded, warning = true, data.NotificationWarning
	common.ApiSuccess(c, data)
}
