package update_account

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

type ResponseOperation = Operation
type HTTP struct {
	factory func(*gin.Context, *domain.User) ResponseOperation
	roles   domain.AdminRoles
}

func NewHTTP(factory func(*gin.Context, *domain.User) ResponseOperation, roles domain.AdminRoles) *HTTP {
	return &HTTP{factory: factory, roles: roles}
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
	response, err := NewUsecase(op, h.roles).Execute(Input{ID: updatedUser.Id, Username: updatedUser.Username, RequestedRole: updatedUser.Role, ChangePassword: updatedUser.Password != "", Permissions: updatedUser.AdminPermissions}, c.GetInt("role"))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, dto.UserFromDomain(response))
}
