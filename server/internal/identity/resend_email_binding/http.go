package resend_email_binding

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	identity             func(*gin.Context) (domain.AuthIdentity, bool)
	invalid, flowInvalid func() error
	resend               func(domain.AuthIdentity, string) (*Output, error)
	writeError           func(*gin.Context, error)
	audit                func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(identity func(*gin.Context) (domain.AuthIdentity, bool), invalid, flowInvalid func() error, resend func(domain.AuthIdentity, string) (*Output, error), writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{identity: identity, invalid: invalid, flowInvalid: flowInvalid, resend: resend, writeError: writeError, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		h.writeError(c, h.invalid())
		return
	}
	succeeded := false
	defer func() { h.audit(c, identity.UserID, "user.email_binding_resend", map[string]any{"success": succeeded}) }()
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.FlowToken == "" {
		h.writeError(c, h.flowInvalid())
		return
	}
	data, err := h.resend(identity, request.FlowToken)
	if err != nil {
		h.writeError(c, err)
		return
	}
	succeeded = true
	common.ApiSuccess(c, data)
}
