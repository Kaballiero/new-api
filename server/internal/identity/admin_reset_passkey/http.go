package admin_reset_passkey

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	factory    func(*gin.Context) Operation
	audit      func(*gin.Context, int, string, map[string]any)
	writeError func(*gin.Context, error)
}

func NewHTTP(factory func(*gin.Context) Operation, audit func(*gin.Context, int, string, map[string]any), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{factory: factory, audit: audit, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyUserIDInvalid)
		return
	}
	result, err := NewUsecase(h.factory(c)).Execute(id)
	if errors.Is(err, domain.ErrTargetForbidden) {
		common.ApiErrorMsg(c, "no permission")
		return
	}
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.audit(c, result.ID, "user.reset_passkey", map[string]any{"username": result.Username, "id": result.ID})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": common.TranslateMessage(c, i18n.MsgIdentityPasskeyReset)})
}
