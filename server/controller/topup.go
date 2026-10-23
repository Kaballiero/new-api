package controller

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"net/http"
	"sync"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"

	paymentsapp "github.com/QuantumNous/new-api/internal/app/payments"
)

var paymentsHTTP = sync.OnceValue(paymentsapp.NewHTTP)

func GetTopUpInfo(c *gin.Context) { paymentsHTTP().Info.Handle(c) }

// BRIDGE: subscription_payment_epay checkout/notify/return; remove when those callers use the payment app directly.
func GetEpayClient() *epay.Client { return paymentsapp.EpayClient() }

func RequestEpay(c *gin.Context) { paymentsHTTP().CreateEpay.Handle(c) }

// BRIDGE: subscription_payment_epay notify/return; remove when those callbacks own their admission wiring.
func LockOrder(tradeNo string)   { paymentsapp.LockOrder(tradeNo) }
func UnlockOrder(tradeNo string) { paymentsapp.UnlockOrder(tradeNo) }

func EpayNotify(c *gin.Context) { paymentsHTTP().EpayNotify.Handle(c) }

func RequestAmount(c *gin.Context) { paymentsHTTP().QuoteEpay.Handle(c) }

func GetUserTopUps(c *gin.Context) { paymentsHTTP().ListOwn.Handle(c) }

func GetAllTopUps(c *gin.Context) { paymentsHTTP().ListAll.Handle(c) }

func AdminCompleteTopUp(c *gin.Context) { paymentsHTTP().Complete.Handle(c) }

func getTopUpQuota(amount int64) (int, error) {
	quota := decimal.NewFromInt(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quota = decimal.NewFromInt(quota.Div(quotaPerUnit).IntPart()).Mul(quotaPerUnit)
	} else {
		quota = quota.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	return common.WalletQuotaFromDecimalStrict(quota)
}

func getMaxTopUpAmount() int64 {
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	maxStoredAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(quotaPerUnit).
		Floor()
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return maxStoredAmount.Add(decimal.NewFromInt(1)).
			Mul(quotaPerUnit).
			Ceil().
			Sub(decimal.NewFromInt(1)).
			IntPart()
	}
	return maxStoredAmount.IntPart()
}

func validateCreditedQuota(quota decimal.Decimal) (int, error) {
	value, err := common.WalletQuotaFromDecimalStrict(quota)
	if err != nil {
		return 0, errors.New("充值额度超出系统可表示范围")
	}
	if value <= 0 {
		return 0, errors.New("充值额度必须大于 0")
	}
	return value, nil
}

func validateTopUpQuota(amount int64) (int, error) {
	quota, err := getTopUpQuota(amount)
	if err == nil && quota > 0 {
		return quota, nil
	}
	maxAmount := getMaxTopUpAmount()
	if maxAmount > 0 && amount > maxAmount {
		return 0, fmt.Errorf("单笔充值数量不能大于 %d", maxAmount)
	}
	return 0, errors.New("充值数量无效")
}

func rejectInvalidCreditedQuota(c *gin.Context, userId int, quota decimal.Decimal) bool {
	creditedQuota, err := validateCreditedQuota(quota)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return true
	}
	return false
}

func rejectInvalidTopUpQuota(c *gin.Context, userId int, amount int64) bool {
	creditedQuota, err := validateTopUpQuota(amount)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return true
	}
	return false
}
