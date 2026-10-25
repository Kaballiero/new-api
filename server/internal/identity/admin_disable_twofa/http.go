package admin_disable_twofa

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
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgIdentityUserIDInvalid)
		return
	}
	err = NewUsecase(h.factory(c)).Execute(id)
	switch {
	case errors.Is(err, domain.ErrTargetNotFound):
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	case errors.Is(err, domain.ErrTargetForbidden):
		common.ApiErrorI18n(c, i18n.MsgIdentityTwoFAManageForbidden)
		return
	case errors.Is(err, domain.ErrTargetTwoFADisabled):
		common.ApiErrorI18n(c, i18n.MsgIdentityTwoFANotEnabled)
		return
	case err != nil:
		h.writeError(c, err)
		return
	}
	h.audit(c, id, "user.2fa_disable", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": common.TranslateMessage(c, i18n.MsgIdentityAdminTwoFADisabled)})
}
