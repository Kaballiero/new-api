package transport

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

func Message(c *gin.Context, err error) string {
	if errors.Is(err, domain.ErrProductName) {
		return i18n.T(c, i18n.MsgSubscriptionsProductName)
	}
	if errors.Is(err, domain.ErrProductAmount) {
		return i18n.T(c, i18n.MsgSubscriptionsProductAmount)
	}
	if errors.Is(err, domain.ErrPancakeBinding) {
		return i18n.T(c, i18n.MsgSubscriptionsPancakeBinding)
	}
	if errors.Is(err, domain.ErrCreateProduct) {
		return i18n.T(c, i18n.MsgSubscriptionsCreateProduct)
	}
	if errors.Is(err, domain.ErrProductCatalog) {
		return i18n.T(c, i18n.MsgSubscriptionsProductCatalog)
	}

	if errors.Is(err, domain.ErrPurchaseLimit) {
		return i18n.T(c, i18n.MsgSubscriptionsPurchaseLimit)
	}
	if errors.Is(err, domain.ErrNoActivePlan) {
		return i18n.T(c, i18n.MsgSubscriptionsNoActivePlan)
	}
	if errors.Is(err, domain.ErrPlanDisabled) {
		return i18n.T(c, i18n.MsgSubscriptionsPlanDisabled)
	}
	if errors.Is(err, domain.ErrBalanceNegativePrice) {
		return i18n.T(c, i18n.MsgSubscriptionsBalanceNegativePrice)
	}
	if errors.Is(err, domain.ErrBalanceDisallowed) {
		return i18n.T(c, i18n.MsgSubscriptionsBalanceDisallowed)
	}
	if errors.Is(err, domain.ErrInsufficientBalance) {
		return i18n.T(c, i18n.MsgSubscriptionsInsufficientBalance)
	}
	if errors.Is(err, domain.ErrInvalidQuotaUnit) {
		return i18n.T(c, i18n.MsgSubscriptionsInvalidQuotaUnit)
	}
	if errors.Is(err, domain.ErrInvalidSubscriptionID) {
		return i18n.T(c, i18n.MsgSubscriptionsInvalidSubscriptionID)
	}
	if errors.Is(err, domain.ErrInvalidID) {
		return i18n.T(c, i18n.MsgSubscriptionsInvalidID)
	}
	if errors.Is(err, domain.ErrTitleRequired) {
		return i18n.T(c, i18n.MsgSubscriptionsTitleRequired)
	}
	if errors.Is(err, domain.ErrNegativePrice) {
		return i18n.T(c, i18n.MsgSubscriptionsNegativePrice)
	}
	if errors.Is(err, domain.ErrHighPrice) {
		return i18n.T(c, i18n.MsgSubscriptionsHighPrice)
	}
	if errors.Is(err, domain.ErrNegativePurchaseLimit) {
		return i18n.T(c, i18n.MsgSubscriptionsNegativePurchaseLimit)
	}
	if errors.Is(err, domain.ErrNegativeTotal) {
		return i18n.T(c, i18n.MsgSubscriptionsNegativeTotal)
	}
	if errors.Is(err, domain.ErrUpgradeGroupMissing) {
		return i18n.T(c, i18n.MsgSubscriptionsUpgradeGroupMissing)
	}
	if errors.Is(err, domain.ErrDowngradeGroupMissing) {
		return i18n.T(c, i18n.MsgSubscriptionsDowngradeGroupMissing)
	}
	if errors.Is(err, domain.ErrInvalidResetSeconds) {
		return i18n.T(c, i18n.MsgSubscriptionsInvalidResetSeconds)
	}
	if errors.Is(err, domain.ErrInvalidUserID) {
		return i18n.T(c, i18n.MsgSubscriptionsInvalidUserID)
	}
	if errors.Is(err, domain.ErrStripePriceMissing) {
		return i18n.T(c, i18n.MsgSubscriptionsStripePriceMissing)
	}
	if errors.Is(err, domain.ErrStripeConfig) {
		return i18n.T(c, i18n.MsgSubscriptionsStripeConfig)
	}
	if errors.Is(err, domain.ErrStripeWebhook) {
		return i18n.T(c, i18n.MsgSubscriptionsStripeWebhook)
	}
	if errors.Is(err, domain.ErrCreemProductMissing) {
		return i18n.T(c, i18n.MsgSubscriptionsCreemProductMissing)
	}
	if errors.Is(err, domain.ErrCreemWebhook) {
		return i18n.T(c, i18n.MsgSubscriptionsCreemWebhook)
	}
	if errors.Is(err, domain.ErrPancakeProductMissing) {
		return i18n.T(c, i18n.MsgSubscriptionsPancakeProductMissing)
	}
	if errors.Is(err, domain.ErrPancakeConfig) {
		return i18n.T(c, i18n.MsgSubscriptionsPancakeConfig)
	}
	if errors.Is(err, domain.ErrUserMissing) {
		return i18n.T(c, i18n.MsgSubscriptionsUserMissing)
	}
	if errors.Is(err, domain.ErrPriceLow) {
		return i18n.T(c, i18n.MsgSubscriptionsPriceLow)
	}
	if errors.Is(err, domain.ErrPayMethod) {
		return i18n.T(c, i18n.MsgSubscriptionsPayMethod)
	}
	if errors.Is(err, domain.ErrCallbackConfig) {
		return i18n.T(c, i18n.MsgSubscriptionsCallbackConfig)
	}
	if errors.Is(err, domain.ErrEpayConfig) {
		return i18n.T(c, i18n.MsgSubscriptionsEpayConfig)
	}
	if errors.Is(err, domain.ErrCreateOrder) {
		return i18n.T(c, i18n.MsgSubscriptionsCreateOrder)
	}
	if errors.Is(err, domain.ErrCheckout) {
		return i18n.T(c, i18n.MsgSubscriptionsCheckout)
	}
	return err.Error()
}
func WriteError(c *gin.Context, err error) { common.ApiErrorMsg(c, Message(c, err)) }
func CheckoutError(c *gin.Context, err error) {
	if errors.Is(err, domain.ErrCreateOrder) || errors.Is(err, domain.ErrCheckout) {
		c.JSON(200, gin.H{"message": "error", "data": Message(c, err)})
		return
	}
	WriteError(c, err)
}

func InvalidParameters(c *gin.Context) {
	common.ApiErrorI18n(c, i18n.MsgSubscriptionsInvalidParameters)
}

func CheckCompliance(c *gin.Context, confirmed func() bool) bool {
	if !confirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return false
	}
	return true
}
