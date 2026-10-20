package get_account

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/dto"
	"github.com/gin-gonic/gin"
)

type ResponseOperation interface {
	Operation
	ResponseAccount() *domain.User
}
type HTTP struct {
	factory      func() ResponseOperation
	capabilities func(int, int) map[string]map[string]bool
	rootRole     int
}

func NewHTTP(factory func() ResponseOperation, capabilities func(int, int) map[string]map[string]bool, rootRole int) *HTTP {
	return &HTTP{factory: factory, capabilities: capabilities, rootRole: rootRole}
}
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	op := h.factory()
	if failure := NewUsecase(op, h.rootRole).Execute(Input{ID: id, ActorRole: c.GetInt("role")}); failure != nil {
		switch failure.Kind {
		case accounts.AdminLoadMissing:
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "user_not_found", i18n.MsgUserNotExists)
		case accounts.AdminForbidden:
			common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "permission_denied", i18n.MsgUserNoPermissionSameLevel)
		default:
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
		}
		return
	}
	user := dto.UserFromDomain(op.ResponseAccount())
	user.AdminPermissions = h.capabilities(user.Id, user.Role)
	common.ApiSuccess(c, user)
}
