package transport

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/payments/domain"

	"github.com/gin-gonic/gin"
)

func Message(c *gin.Context, err error) string {
	var minimum domain.MinimumAmount
	if errors.As(err, &minimum) {
		return i18n.T(c, i18n.MsgPaymentsMinimum, map[string]any{"Amount": minimum.Amount})
	}
	var maximum domain.MaximumAmount
	if errors.As(err, &maximum) {
		return i18n.T(c, i18n.MsgPaymentsMaximum, map[string]any{"Amount": maximum.Amount})
	}
	switch {
	case errors.Is(err, domain.ErrReadQuery):
		return i18n.T(c, i18n.MsgPaymentsReadQuery)
	case errors.Is(err, domain.ErrInvalidParameters):
		return i18n.T(c, i18n.MsgPaymentsInvalidParameters)
	case errors.Is(err, domain.ErrGroup):
		return i18n.T(c, i18n.MsgPaymentsGroup)
	case errors.Is(err, domain.ErrLowAmount):
		return i18n.T(c, i18n.MsgPaymentsLowAmount)
	case errors.Is(err, domain.ErrUser):
		return i18n.T(c, i18n.MsgPaymentsUser)
	case errors.Is(err, domain.ErrChannel):
		return i18n.T(c, i18n.MsgPaymentsChannel)
	case errors.Is(err, domain.ErrCheckout):
		return i18n.T(c, i18n.MsgPaymentsCheckout)
	case errors.Is(err, domain.ErrInsert):
		return i18n.T(c, i18n.MsgPaymentsInsert)
	case errors.Is(err, domain.ErrQuotaRange):
		return i18n.T(c, i18n.MsgPaymentsQuotaRange)
	case errors.Is(err, domain.ErrQuotaPositive):
		return i18n.T(c, i18n.MsgPaymentsQuotaPositive)
	case errors.Is(err, domain.ErrAmount):
		return i18n.T(c, i18n.MsgPaymentsAmount)
	case errors.Is(err, domain.ErrMethod):
		return i18n.T(c, i18n.MsgPaymentsMethod)
	case errors.Is(err, domain.ErrEpayConfig):
		return i18n.T(c, i18n.MsgPaymentsEpayConfig)
	case errors.Is(err, domain.ErrStripeMaximum):
		return i18n.T(c, i18n.MsgPaymentsStripeMaximum)
	case errors.Is(err, domain.ErrSuccessURL):
		return i18n.T(c, i18n.MsgPaymentsSuccessURL)
	case errors.Is(err, domain.ErrCancelURL):
		return i18n.T(c, i18n.MsgPaymentsCancelURL)
	case errors.Is(err, domain.ErrProductRequired):
		return i18n.T(c, i18n.MsgPaymentsProductRequired)
	case errors.Is(err, domain.ErrProductsConfig):
		return i18n.T(c, i18n.MsgPaymentsProductsConfig)
	case errors.Is(err, domain.ErrProductMissing):
		return i18n.T(c, i18n.MsgPaymentsProductMissing)
	case errors.Is(err, domain.ErrWaffoDisabled):
		return i18n.T(c, i18n.MsgPaymentsWaffoDisabled)
	case errors.Is(err, domain.ErrPayMethod):
		return i18n.T(c, i18n.MsgPaymentsPayMethod)
	case errors.Is(err, domain.ErrPaymentConfig):
		return i18n.T(c, i18n.MsgPaymentsPaymentConfig)
	case errors.Is(err, domain.ErrPancakeConfig):
		return i18n.T(c, i18n.MsgPaymentsPancakeConfig)
	case errors.Is(err, domain.ErrCredentials):
		return i18n.T(c, i18n.MsgPaymentsCredentials)
	case errors.Is(err, domain.ErrSaveConfig):
		return i18n.T(c, i18n.MsgPaymentsSaveConfig)
	case errors.Is(err, domain.ErrCatalog):
		return i18n.T(c, i18n.MsgPaymentsCatalog)
	case errors.Is(err, domain.ErrSearch):
		return i18n.T(c, i18n.MsgPaymentsSearch)
	case errors.Is(err, domain.ErrTradeNo):
		return i18n.T(c, i18n.MsgPaymentsTradeNo)
	case errors.Is(err, domain.ErrPaymentTradeNo):
		return i18n.T(c, i18n.MsgPaymentsPaymentTradeNo)
	case errors.Is(err, domain.ErrOrderMissing):
		return i18n.T(c, i18n.MsgPaymentsOrderMissing)
	case errors.Is(err, domain.ErrManualStatus):
		return i18n.T(c, i18n.MsgPaymentsManualStatus)
	case errors.Is(err, domain.ErrOrderStatus):
		return i18n.T(c, i18n.MsgPaymentsOrderStatus)
	case errors.Is(err, domain.ErrRecharge):
		return i18n.T(c, i18n.MsgPaymentsRecharge)
	case errors.Is(err, domain.ErrCompliance):
		return i18n.T(c, i18n.MsgPaymentsCompliance)
	case errors.Is(err, domain.ErrInvalidQuota):
		return i18n.T(c, i18n.MsgPaymentsInvalidQuota)
	case errors.Is(err, domain.ErrWalletLimit):
		return i18n.T(c, i18n.MsgPaymentsWalletLimit)
	case errors.Is(err, domain.ErrProviderMismatch):
		return i18n.T(c, i18n.MsgPaymentsProviderMismatch)
	case errors.Is(err, domain.ErrNotFound):
		return i18n.T(c, i18n.MsgPaymentsNotFound)
	case errors.Is(err, domain.ErrNotPending):
		return i18n.T(c, i18n.MsgPaymentsNotPending)
	case errors.Is(err, domain.ErrSessionOnly):
		return i18n.T(c, i18n.MsgPaymentsSessionOnly)
	default:
		return err.Error()
	}
}

func WriteError(c *gin.Context, err error) { common.ApiErrorMsg(c, Message(c, err)) }

// Context avoids a typed-nil Gin context at diagnostic boundaries.
func Context(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}
