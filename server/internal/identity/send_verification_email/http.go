package send_verification_email

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	operation          EmailVerificationOperation
	writeSecurityError func(*gin.Context, error)
}

func NewHTTP(op EmailVerificationOperation, writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{operation: op, writeSecurityError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	err := NewUsecase(h.operation).Execute(Input{Email: c.Query("email"), Lang: i18n.GetLangFromContext(c)})
	switch {
	case errors.Is(err, domain.ErrEmailInvalid), errors.Is(err, domain.ErrEmailRestricted):
		h.writeSecurityError(c, err)
		return
	case errors.Is(err, domain.ErrEmailTaken):
		common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		return
	case err != nil:
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
