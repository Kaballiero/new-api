package manage_account

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	factory func(*gin.Context) Operation
	roles   domain.AdminRoles
	quota   func(*gin.Context, ManageRequest)
}

func NewHTTP(factory func(*gin.Context) Operation, roles domain.AdminRoles, quota func(*gin.Context, ManageRequest)) *HTTP {
	return &HTTP{factory: factory, roles: roles, quota: quota}
}

// ManageUser Only admin user can do this
func (h *HTTP) Handle(c *gin.Context) {
	var req ManageRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	if req.Action == "add_quota" {
		h.quota(c, req)
		return
	}
	actorRole := c.GetInt("role")
	result, failure := NewUsecase(h.factory(c), h.roles).Execute(Input{ID: req.Id, Action: req.Action}, actorRole)
	if failure != nil {
		switch failure.Kind {
		case accounts.AdminLoadMissing:
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "user_not_found", i18n.MsgUserNotExists)
		case accounts.AdminLoadFailed, accounts.AdminOrdinaryWriteFailed:
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
		case accounts.AdminForbidden:
			common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "permission_denied", i18n.MsgUserNoPermissionHigherLevel)
		case accounts.AdminCannotDisableRoot:
			common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "cannot_disable_root", i18n.MsgUserCannotDisableRootUser)
		case accounts.AdminCannotDeleteRoot:
			common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "cannot_delete_root", i18n.MsgUserCannotDeleteRootUser)
		case accounts.AdminCannotPromote:
			common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "admin_cannot_promote", i18n.MsgUserAdminCannotPromote)
		case accounts.AdminAlreadyAdmin:
			common.ApiErrorI18nStatusCode(c, http.StatusConflict, "user_already_admin", i18n.MsgUserAlreadyAdmin)
		case accounts.AdminCannotDemoteRoot:
			common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "cannot_demote_root", i18n.MsgUserCannotDemoteRootUser)
		case accounts.AdminAlreadyCommon:
			common.ApiErrorI18nStatusCode(c, http.StatusConflict, "user_already_common", i18n.MsgUserAlreadyCommon)
		case accounts.AdminInvalidAction:
			common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		default:
			common.ApiError(c, failure.Cause)
		}
		return
	}
	if result.Deleted {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
		return
	}
	common.ApiSuccess(c, ManageUserResponse{Action: result.Action, UserId: result.UserID, Role: result.Role, Status: result.Status})
}
