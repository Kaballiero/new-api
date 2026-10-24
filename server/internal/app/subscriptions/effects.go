package subscriptions

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

type effects struct{}

func (effects) RefreshGroup(userID int, operation string) {
	if err := model.RefreshUserGroupCache(userID); err != nil {
		common.SysError(fmt.Sprintf("failed to refresh user group cache after %s for user %d: %v", operation, userID, err))
	}
}
func (effects) Purchase(r domain.PurchaseReceipt) {
	model.RecordLog(
		r.UserID,
		model.LogTypeTopup,
		i18n.Translate(
			receiptLanguage(r.UserID),
			i18n.MsgSubscriptionsReceiptPurchase,
			map[string]any{
				"Plan": r.PlanTitle,
				"Money": fmt.Sprintf(
					"%.2f",
					r.Money,
				),
				"Method": r.PaymentMethod,
			},
		),
	)
}
func (effects) BalancePurchase(r domain.BalanceReceipt) {
	model.RecordLog(
		r.UserID,
		model.LogTypeTopup,
		i18n.Translate(
			receiptLanguage(r.UserID),
			i18n.MsgSubscriptionsReceiptBalance,
			map[string]any{
				"Plan": r.PlanTitle,
				"Money": fmt.Sprintf(
					"%.2f",
					r.Money,
				),
				"Quota": r.Quota,
			},
		),
	)
}
func (effects) DebitCache(userID, quota int) {
	if err := model.CacheDecrUserQuota(userID, int64(quota)); err != nil {
		common.SysLog("failed to decrease user quota cache after subscription balance purchase: " + err.Error())
	}
}
func balanceQuota(price float64) (int, error) {
	if price <= 0 {
		return 0, nil
	}
	if common.QuotaPerUnit <= 0 {
		return 0, domain.ErrInvalidQuotaUnit
	}
	return common.WalletQuotaFromDecimalStrict(decimal.NewFromFloat(price).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Ceil())
}

func receiptLanguage(userID int) string {
	return i18n.ParseAcceptLanguage(model.GetUserLanguage(userID))
}
