package delete_account

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	operation   Operation
	authorize   func(*gin.Context) (domain.AuthIdentity, bool)
	clearCookie func(*gin.Context)
	writeError  func(*gin.Context, error)
	audit       func(*gin.Context, int, string, map[string]any)
}

// NewHTTP builds the handler; authorize writes its own response and returns
// false when the security proof is missing or rejected.
func NewHTTP(op Operation, authorize func(*gin.Context) (domain.AuthIdentity, bool), clearCookie func(*gin.Context), writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{operation: op, authorize: authorize, clearCookie: clearCookie, writeError: writeError, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	succeeded := false
	defer func() { h.audit(c, c.GetInt("id"), "user.account_delete", map[string]any{"success": succeeded}) }()
	identity, ok := h.authorize(c)
	if !ok {
		return
	}
	err := NewUsecase(h.operation).Execute(Input{Identity: identity})
	if errors.Is(err, domain.ErrRootDeletion) {
		common.ApiErrorI18n(c, i18n.MsgUserCannotDeleteRootUser)
		return
	}
	if err != nil {
		h.writeError(c, err)
		return
	}
	succeeded = true
	h.clearCookie(c)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{}})
}
