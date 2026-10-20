package send_verification_email

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	operation          EmailVerificationOperation
	writeSecurityError func(*gin.Context, error)
}

func NewHTTP(op EmailVerificationOperation, writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{operation: op, writeSecurityError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	result, err := NewUsecase(h.operation).Execute(c.Query("email"))
	if result.ValidationRejected {
		h.writeSecurityError(c, err)
		return
	}
	if result.EmailTaken {
		common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
