package request_password_reset

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	operation     func(context.Context) PasswordResetEmailOperation
	emailNotFound func() error
}

func NewHTTP(op func(context.Context) PasswordResetEmailOperation, notFound func() error) *HTTP {
	return &HTTP{operation: op, emailNotFound: notFound}
}
func (h *HTTP) Handle(c *gin.Context) {
	input := Input{Email: c.Query("email")}
	if NewUsecase(h.operation(c.Request.Context()), h.emailNotFound).Execute(input.Email) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
