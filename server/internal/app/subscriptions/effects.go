package subscriptions

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
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
	model.RecordLog(r.UserID, model.LogTypeTopup, fmt.Sprintf("订阅购买成功，套餐: %s，支付金额: %.2f，支付方式: %s", r.PlanTitle, r.Money, r.PaymentMethod))
}
func (effects) BalancePurchase(r domain.BalanceReceipt) {
	model.RecordLog(r.UserID, model.LogTypeTopup, fmt.Sprintf("使用余额购买订阅成功，套餐: %s，支付金额: %.2f，扣除额度: %d", r.PlanTitle, r.Money, r.Quota))
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
