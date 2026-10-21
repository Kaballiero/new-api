package manage_account

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/transport"
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
	result, err := NewUsecase(h.factory(c), h.roles).Execute(Input{ID: req.Id, Action: req.Action}, actorRole)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	if result.Deleted {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
		return
	}
	common.ApiSuccess(c, ManageUserResponse{Action: result.Action, UserId: result.UserID, Role: result.Role, Status: result.Status})
}
