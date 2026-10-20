package delete_account

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct{ factory func(*gin.Context) Operation }

func NewHTTP(factory func(*gin.Context) Operation) *HTTP { return &HTTP{factory: factory} }
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	actorRole := c.GetInt("role")
	if failure := NewUsecase(h.factory(c)).Execute(Input{ID: id, ActorRole: actorRole}); failure != nil {
		switch failure.Kind {
		case accounts.AdminLoadMissing:
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "user_not_found", i18n.MsgUserNotExists)
		case accounts.AdminForbidden:
			common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "permission_denied", i18n.MsgUserNoPermissionHigherLevel)
		default:
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
		}
		return
	}
	common.ApiSuccess(c, nil)
}
