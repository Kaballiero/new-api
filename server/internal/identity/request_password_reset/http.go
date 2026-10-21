package request_password_reset

import (
	"context"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	operation func(context.Context) PasswordResetEmailOperation
}

func NewHTTP(op func(context.Context) PasswordResetEmailOperation) *HTTP {
	return &HTTP{operation: op}
}
func (h *HTTP) Handle(c *gin.Context) {
	input := Input{Email: c.Query("email"), Lang: i18n.GetLangFromContext(c)}
	if NewUsecase(h.operation(c.Request.Context())).Execute(input) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
