package billing

import (
	plancache "github.com/QuantumNous/new-api/internal/adapter/cache/subscriptions"
	keyquota "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys"
	keyrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/billing/domain"
)

// WalletCapabilities retain the existing account balance mutations without assigning them to subscriptions.
type WalletCapabilities struct {
	Balance func(int) (int, error)
	Debit   func(int, int) error
	Credit  func(int, int) error
}

type Store struct {
	Wallet            WalletCapabilities
	Reserves          ReserveStore
	Keys              keyrecord.KeyReader
	KeyQuotas         keyquota.QuotaStore
	SubscriptionPlans *plancache.Store
	DatabaseTime      func() int64
}

func (s Store) WalletBalance(id int) (int, error) { return s.Wallet.Balance(id) }
func (s Store) ReserveWallet(id, amount int) (bool, error) {
	return s.Reserves.TryReserveUserQuota(id, amount)
}
func (s Store) DebitWallet(id, amount int) error  { return s.Wallet.Debit(id, amount) }
func (s Store) CreditWallet(id, amount int) error { return s.Wallet.Credit(id, amount) }
func (s Store) ReserveToken(account domain.Account, amount int) (bool, error) {
	return s.Reserves.TryReserveTokenQuota(account.TokenID, account.TokenKey, amount, account.Unlimited)
}
func (s Store) TokenRemaining(key string) int {
	token, err := s.Keys.Read(key, false)
	if err != nil || token == nil {
		return 0
	}
	return token.RemainQuota
}
func (s Store) DebitToken(account domain.Account, amount int) error {
	return s.KeyQuotas.DecreaseTokenQuota(account.TokenID, account.TokenKey, amount)
}
func (s Store) CreditToken(account domain.Account, amount int) error {
	return s.KeyQuotas.IncreaseTokenQuota(account.TokenID, account.TokenKey, amount)
}
func (s Store) HasSubscription(id int) (bool, error) { return hasActiveUserSubscription(id) }
func (s Store) AllowsWalletOverflow(id int) (bool, error) {
	return userActiveSubscriptionsAllowWalletOverflow(id)
}
func (s Store) ReserveSubscription(requestID string, userID int, _ string, amount int64) (domain.SubscriptionHold, error) {
	result, err := s.preConsumeUserSubscription(requestID, userID, amount)
	if err != nil {
		return domain.SubscriptionHold{}, err
	}
	hold := domain.SubscriptionHold{ID: result.UserSubscriptionId, PreConsumed: result.PreConsumed, AmountTotal: result.AmountTotal, AmountUsedAfter: result.AmountUsedAfter}
	if plan, err := s.getSubscriptionPlanInfoByUserSubscriptionId(result.UserSubscriptionId); err == nil && plan != nil {
		hold.PlanID, hold.PlanTitle = plan.PlanId, plan.PlanTitle
	}
	return hold, nil
}
func (s Store) AdjustSubscription(id int, delta int64) error {
	return postConsumeUserSubscriptionDelta(id, delta)
}
func (s Store) RefundSubscription(requestID string) error {
	return refundSubscriptionPreConsume(requestID)
}
