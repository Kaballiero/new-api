package subscriptions

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/usagestorage"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"

	"github.com/shopspring/decimal"
)

type effects struct{}

func (effects) RefreshGroup(userID int, operation string) {
	if err := accountstorage.Cache().RefreshUserGroupCache(userID); err != nil {
		common.SysError(fmt.Sprintf("failed to refresh user group cache after %s for user %d: %v", operation, userID, err))
	}
}
func (effects) Purchase(r domain.PurchaseReceipt) {
	usagestorage.Writer().RecordLog(
		r.UserID,
		usagerecord.LogTypeTopup,
		i18n.Translate(
			receiptLanguage(r.UserID),
			i18n.MsgSubscriptionsReceiptPurchase,
			map[string]any{
				"Plan":   r.PlanTitle,
				"Money":  fmt.Sprintf("%.2f", r.Money),
				"Method": r.PaymentMethod,
			},
		),
	)
}
func (effects) BalancePurchase(r domain.BalanceReceipt) {
	usagestorage.Writer().RecordLog(
		r.UserID,
		usagerecord.LogTypeTopup,
		i18n.Translate(
			receiptLanguage(r.UserID),
			i18n.MsgSubscriptionsReceiptBalance,
			map[string]any{
				"Plan":  r.PlanTitle,
				"Money": fmt.Sprintf("%.2f", r.Money),
				"Quota": r.Quota,
			},
		),
	)
}
func (effects) DebitCache(userID, quota int) {
	if err := accountstorage.Cache().CacheDecrUserQuota(userID, int64(quota)); err != nil {
		common.SysLog("failed to decrease user quota cache after subscription balance purchase: " + err.Error())
	}
}

type quotaMath struct{}

func (quotaMath) QuotaPerUnit() float64 { return common.QuotaPerUnit }
func (quotaMath) Strict(quota decimal.Decimal) (int, error) {
	return common.WalletQuotaFromDecimalStrict(quota)
}

func receiptLanguage(userID int) string {
	return i18n.ParseAcceptLanguage(accountstorage.Cache().GetUserLanguage(userID))
}
