package transport

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/gin-gonic/gin"
)

func WriteError(c *gin.Context, err error) {
	if errors.Is(err, domain.ErrInvalidUserID) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsInvalidUserID)
		return
	}
	common.ApiError(c, err)
}

func InvalidParameters(c *gin.Context) {
	common.ApiErrorI18n(c, i18n.MsgSubscriptionsInvalidParameters)
}
