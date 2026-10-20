package update_account

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/dto"
	"github.com/gin-gonic/gin"
)

type ResponseOperation = Operation
type HTTP struct {
	factory       func(*gin.Context, *domain.User) ResponseOperation
	roles         domain.AdminRoles
	usernameTaken func() error
}

func NewHTTP(factory func(*gin.Context, *domain.User) ResponseOperation, roles domain.AdminRoles, usernameTaken func() error) *HTTP {
	return &HTTP{factory: factory, roles: roles, usernameTaken: usernameTaken}
}
func (h *HTTP) Handle(c *gin.Context) {
	var updatedUser dto.User
	err := common.DecodeJson(c.Request.Body, &updatedUser)
	if err != nil || updatedUser.Id == 0 {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	updatedUser.Username = strings.TrimSpace(updatedUser.Username)
	if updatedUser.Username == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.StructExcept(&updatedUser, "Password"); err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusUnprocessableEntity, "user_input_invalid", i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	op := h.factory(c, updatedUser.Domain())
	response, failure := NewUsecase(op, h.roles).Execute(Input{ID: updatedUser.Id, Username: updatedUser.Username, RequestedRole: updatedUser.Role, ChangePassword: updatedUser.Password != "", Permissions: updatedUser.AdminPermissions}, c.GetInt("role"))
	if failure != nil {
		switch failure.Kind {
		case accounts.AdminInvalidRole:
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		case accounts.AdminForbidden:
			common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "permission_denied", i18n.MsgUserNoPermissionHigherLevel)
		case accounts.AdminLoadMissing:
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "user_not_found", i18n.MsgUserNotExists)
		case accounts.AdminLoadFailed:
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
		default:
			if failure.Kind == accounts.AdminWriteFailed && errors.Is(failure.Cause, h.usernameTaken()) {
				common.ApiErrorI18nStatusCode(c, http.StatusConflict, "username_already_taken", i18n.MsgUserExists)
			} else {
				common.ApiError(c, failure.Cause)
			}
		}
		return
	}
	common.ApiSuccess(c, dto.UserFromDomain(response))
}
