package recover_password

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	operation PasswordRecoveryOperation
	errors    PasswordRecoveryErrors
}

func NewHTTP(op PasswordRecoveryOperation, e PasswordRecoveryErrors) *HTTP {
	return &HTTP{operation: op, errors: e}
}
func (h *HTTP) Handle(c *gin.Context) {
	var req PasswordResetRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := NewUsecase(h.operation, h.errors).Execute(req.Email, req.Token)
	if result.InvalidInput {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if result.InvalidLink {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordResetLinkInvalid)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result.Password})
}
