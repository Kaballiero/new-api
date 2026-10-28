package recover_password

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	operation PasswordRecoveryOperation
}

func NewHTTP(op PasswordRecoveryOperation) *HTTP {
	return &HTTP{operation: op}
}
func (h *HTTP) Handle(c *gin.Context) {
	var req PasswordResetRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	password, err := NewUsecase(h.operation).Execute(req.Email, req.Token)
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	case errors.Is(err, domain.ErrRecoveryLinkInvalid):
		common.ApiErrorI18n(c, i18n.MsgUserPasswordResetLinkInvalid)
		return
	case err != nil:
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": password})
}
