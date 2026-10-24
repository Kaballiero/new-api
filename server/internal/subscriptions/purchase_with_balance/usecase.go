package purchase_with_balance

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

type Input struct{ UserID, PlanID int }

type QuotaMath interface {
	QuotaPerUnit() float64
	Strict(decimal.Decimal) (int, error)
}

type Store interface {
	Balance(func(domain.BalanceTx) error) error
}
type Effects interface {
	DebitCache(userID, quota int)
	RefreshGroup(userID int, operation string)
	BalancePurchase(domain.BalanceReceipt)
}
type Usecase struct {
	store                     Store
	effects                   Effects
	quota                     QuotaMath
	now                       func() int64
	reference                 func(int) string
	method, provider, success string
}

func NewUsecase(
	store Store,
	effects Effects,
	quota QuotaMath,
	now func() int64,
	reference func(int) string,
	method, provider, success string,
) Usecase {
	return Usecase{store, effects, quota, now, reference, method, provider, success}
}
func (u Usecase) Execute(in Input) error {
	userID, planID := in.UserID, in.PlanID
	if userID <= 0 || planID <= 0 {
		return errors.New("invalid userId or planId")
	}
	var receipt domain.BalanceReceipt
	upgrade := ""
	err := u.store.Balance(func(tx domain.BalanceTx) error {
		plan, err := tx.PlanByID(planID)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			return domain.ErrPlanDisabled
		}
		if plan.PriceAmount < 0 {
			return domain.ErrBalanceNegativePrice
		}
		if plan.AllowBalancePay != nil && !*plan.AllowBalancePay {
			return domain.ErrBalanceDisallowed
		}
		quota := 0
		if plan.PriceAmount != 0 {
			if u.quota.QuotaPerUnit() <= 0 {
				return domain.ErrInvalidQuotaUnit
			}
			quota, err = u.quota.Strict(decimal.NewFromFloat(plan.PriceAmount).Mul(decimal.NewFromFloat(u.quota.QuotaPerUnit())).Ceil())
			if err != nil {
				return err
			}
		}
		balance, err := tx.WalletBalance(userID)
		if err != nil {
			return err
		}
		if quota > 0 && balance < quota {
			return domain.ErrInsufficientBalance
		}
		if quota > 0 {
			if err := tx.DebitWallet(userID, quota); err != nil {
				return err
			}
		}
		sub, err := domain.GrantSubscription(tx, userID, plan, u.method, u.now)
		if err != nil {
			return err
		}
		now := u.now()
		order := domain.SubscriptionOrder{
			UserId:          userID,
			PlanId:          plan.Id,
			Money:           plan.PriceAmount,
			TradeNo:         u.reference(userID),
			PaymentMethod:   u.method,
			PaymentProvider: u.provider,
			Status:          u.success,
			CreateTime:      now,
			CompleteTime:    now,
			ProviderPayload: fmt.Sprintf("charged_quota=%d", quota),
		}
		if err := tx.InsertOrder(&order); err != nil {
			return err
		}
		receipt = domain.BalanceReceipt{UserID: userID, PlanTitle: plan.Title, Money: plan.PriceAmount, Quota: quota}
		if sub.PrevUserGroup != "" {
			upgrade = strings.TrimSpace(sub.UpgradeGroup)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if receipt.Quota > 0 {
		u.effects.DebitCache(userID, receipt.Quota)
	}
	if upgrade != "" {
		u.effects.RefreshGroup(userID, "subscription balance purchase")
	}
	u.effects.BalancePurchase(receipt)
	return nil
}
