package transport

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/gin-gonic/gin"
)

func WriteError(c *gin.Context, err error) {
	if errors.Is(err, domain.ErrPurchaseLimit) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsPurchaseLimit)
		return
	}
	if errors.Is(err, domain.ErrNoActivePlan) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsNoActivePlan)
		return
	}
	if errors.Is(err, domain.ErrPlanDisabled) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsPlanDisabled)
		return
	}
	if errors.Is(err, domain.ErrBalanceNegativePrice) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsBalanceNegativePrice)
		return
	}
	if errors.Is(err, domain.ErrBalanceDisallowed) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsBalanceDisallowed)
		return
	}
	if errors.Is(err, domain.ErrInsufficientBalance) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsInsufficientBalance)
		return
	}
	if errors.Is(err, domain.ErrInvalidQuotaUnit) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsInvalidQuotaUnit)
		return
	}
	if errors.Is(err, domain.ErrInvalidSubscriptionID) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsInvalidSubscriptionID)
		return
	}

	if errors.Is(err, domain.ErrInvalidID) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsInvalidID)
		return
	}
	if errors.Is(err, domain.ErrTitleRequired) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsTitleRequired)
		return
	}
	if errors.Is(err, domain.ErrNegativePrice) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsNegativePrice)
		return
	}
	if errors.Is(err, domain.ErrHighPrice) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsHighPrice)
		return
	}
	if errors.Is(err, domain.ErrNegativePurchaseLimit) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsNegativePurchaseLimit)
		return
	}
	if errors.Is(err, domain.ErrNegativeTotal) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsNegativeTotal)
		return
	}
	if errors.Is(err, domain.ErrUpgradeGroupMissing) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsUpgradeGroupMissing)
		return
	}
	if errors.Is(err, domain.ErrDowngradeGroupMissing) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsDowngradeGroupMissing)
		return
	}
	if errors.Is(err, domain.ErrInvalidResetSeconds) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsInvalidResetSeconds)
		return
	}

	if errors.Is(err, domain.ErrInvalidUserID) {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionsInvalidUserID)
		return
	}
	common.ApiError(c, err)
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
