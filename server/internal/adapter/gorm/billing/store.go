package billing

import (
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/model"
)

// Store delegates to shared quota primitives. Their SQL, Redis Lua, batch
// queue, hydration, fallback and compensation order remain owned by model.
type Store struct{}

func (Store) WalletBalance(id int) (int, error) { return model.GetUserQuota(id, false) }
func (Store) ReserveWallet(id, amount int) (bool, error) {
	return model.TryReserveUserQuota(id, amount)
}
func (Store) DebitWallet(id, amount int) error  { return model.DecreaseUserQuota(id, amount, false) }
func (Store) CreditWallet(id, amount int) error { return model.IncreaseUserQuota(id, amount, false) }
func (Store) ReserveToken(account domain.Account, amount int) (bool, error) {
	return model.TryReserveTokenQuota(account.TokenID, account.TokenKey, amount, account.Unlimited)
}
func (Store) TokenRemaining(key string) int {
	token, err := model.GetTokenByKey(key, false)
	if err != nil || token == nil {
		return 0
	}
	return token.RemainQuota
}
func (Store) DebitToken(account domain.Account, amount int) error {
	return model.DecreaseTokenQuota(account.TokenID, account.TokenKey, amount)
}
func (Store) CreditToken(account domain.Account, amount int) error {
	return model.IncreaseTokenQuota(account.TokenID, account.TokenKey, amount)
}
func (Store) HasSubscription(id int) (bool, error) { return model.HasActiveUserSubscription(id) }
func (Store) AllowsWalletOverflow(id int) (bool, error) {
	return model.UserActiveSubscriptionsAllowWalletOverflow(id)
}
func (Store) ReserveSubscription(requestID string, userID int, modelName string, amount int64) (domain.SubscriptionHold, error) {
	result, err := model.PreConsumeUserSubscription(requestID, userID, modelName, 0, amount)
	if err != nil {
		return domain.SubscriptionHold{}, err
	}
	hold := domain.SubscriptionHold{ID: result.UserSubscriptionId, PreConsumed: result.PreConsumed, AmountTotal: result.AmountTotal, AmountUsedAfter: result.AmountUsedAfter}
	if plan, err := model.GetSubscriptionPlanInfoByUserSubscriptionId(result.UserSubscriptionId); err == nil && plan != nil {
		hold.PlanID, hold.PlanTitle = plan.PlanId, plan.PlanTitle
	}
	return hold, nil
}
func (Store) AdjustSubscription(id int, delta int64) error {
	return model.PostConsumeUserSubscriptionDelta(id, delta)
}
func (Store) RefundSubscription(requestID string) error {
	return model.RefundSubscriptionPreConsume(requestID)
}
