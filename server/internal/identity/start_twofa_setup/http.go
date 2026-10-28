package start_twofa_setup

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	authorize  func(*gin.Context) (domain.AuthIdentity, *domain.FlowAuthorization, bool)
	start      func(domain.AuthIdentity, *domain.FlowAuthorization) (*Output, error)
	writeError func(*gin.Context, error)
	audit      func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(authorize func(*gin.Context) (domain.AuthIdentity, *domain.FlowAuthorization, bool), start func(domain.AuthIdentity, *domain.FlowAuthorization) (*Output, error), writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{authorize: authorize, start: start, writeError: writeError, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, authorization, ok := h.authorize(c)
	if !ok {
		return
	}
	setup, err := h.start(identity, authorization)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.audit(c, identity.UserID, "user.2fa_setup", nil)
	common.ApiSuccess(c, setup)
}
