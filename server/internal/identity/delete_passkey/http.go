package delete_passkey

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/transport"

	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Bundle() *dto.AuthBundle
}
type HTTP struct {
	userID     func(*gin.Context) (int, error)
	authorize  func(*gin.Context) bool
	identity   func(*gin.Context) (domain.AuthIdentity, bool)
	factory    func(Input) Completion
	writeError func(*gin.Context, error)
	audit      func(*gin.Context, int, string, map[string]any)
}

// NewHTTP builds the handler; userID returns the enabled authenticated user,
// authorize writes its own response and returns false when the security proof
// is missing or rejected.
func NewHTTP(userID func(*gin.Context) (int, error), authorize func(*gin.Context) bool, identity func(*gin.Context) (domain.AuthIdentity, bool), factory func(Input) Completion, writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{userID: userID, authorize: authorize, identity: identity, factory: factory, writeError: writeError, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	userID, err := h.userID(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if !h.authorize(c) {
		return
	}
	identity, ok := h.identity(c)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgIdentitySecurityVerificationUnsupported)
		return
	}
	op := h.factory(Input{Identity: identity})
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	h.audit(c, userID, "user.passkey_delete", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": common.TranslateMessage(c, i18n.MsgIdentityPasskeyUnbound), "data": transport.RotationData(op.Bundle())})
}
