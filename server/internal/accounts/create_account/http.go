package create_account

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/dto"
	"github.com/QuantumNous/new-api/internal/accounts/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	factory func(*gin.Context) Operation
	roles   domain.AdminRoles
}

func NewHTTP(factory func(*gin.Context) Operation, roles domain.AdminRoles) *HTTP {
	return &HTTP{factory: factory, roles: roles}
}
func (h *HTTP) Handle(c *gin.Context) {
	var user dto.User
	err := common.DecodeJson(c.Request.Body, &user)
	user.Username = strings.TrimSpace(user.Username)
	if err != nil || user.Username == "" || user.Password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	actorRole := c.GetInt("role")
	err = NewUsecase(h.factory(c), h.roles).Execute(Input{Username: user.Username, Password: user.Password, DisplayName: user.DisplayName, Role: user.Role, Permissions: user.AdminPermissions}, actorRole)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
