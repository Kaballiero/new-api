package complete_order

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"strings"
)

// Store retains the global cached plan lookup inside the active order transaction.
type Store interface {
	PlanByID(planID int) (*domain.SubscriptionPlan, error)
	Complete(run func(domain.CompleteTx) error) error
}

// Effects refreshes and records only after the complete transaction commits.
type Effects interface {
	RefreshGroup(userID int, operation string)
	Purchase(receipt domain.PurchaseReceipt)
}
type Input = domain.CompletionInput
type Usecase struct {
	store   Store
	effects Effects
	states  domain.OrderStates
	now     func() int64
}

func NewUsecase(store Store, effects Effects, states domain.OrderStates, now func() int64) Usecase {
	return Usecase{store: store, effects: effects, states: states, now: now}
}
func (u Usecase) Execute(in Input) error {
	if in.TradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	var receipt domain.PurchaseReceipt
	upgradeGroup := ""
	err := u.store.Complete(func(tx domain.CompleteTx) error {
		order, err := tx.LockOrder(in.TradeNo)
		if err != nil {
			return domain.ErrOrderNotFound
		}
		if in.ExpectedProvider != "" && order.PaymentProvider != in.ExpectedProvider {
			return domain.ErrProviderMismatch
		}
		if order.Status == u.states.Success {
			return nil
		}
		if order.Status != u.states.Pending {
			return domain.ErrOrderStatusInvalid
		}
		plan, err := u.store.PlanByID(order.PlanId)
		if err != nil {
			return err
		}
		// Already-purchased orders still complete when the plan has been disabled.
		if err := tx.LockUserID(order.UserId); err != nil {
			return err
		}
		sub, err := domain.GrantSubscription(tx, order.UserId, plan, "order", u.now)
		if err != nil {
			return err
		}
		if sub.PrevUserGroup != "" {
			upgradeGroup = strings.TrimSpace(sub.UpgradeGroup)
		}
		// History deliberately precedes actual-method replacement for Epay compatibility.
		if err := tx.UpsertHistory(*order); err != nil {
			return err
		}
		order.Status = u.states.Success
		order.CompleteTime = u.now()
		if in.ProviderPayload != "" {
			order.ProviderPayload = in.ProviderPayload
		}
		if in.ActualMethod != "" && order.PaymentMethod != in.ActualMethod {
			order.PaymentMethod = in.ActualMethod
		}
		if err := tx.SaveOrder(order); err != nil {
			return err
		}
		receipt = domain.PurchaseReceipt{UserID: order.UserId, PlanTitle: plan.Title, Money: order.Money, PaymentMethod: order.PaymentMethod}
		return nil
	})
	if err != nil {
		return err
	}
	if upgradeGroup != "" && receipt.UserID > 0 {
		u.effects.RefreshGroup(receipt.UserID, "subscription payment completion")
	}
	if receipt.UserID > 0 {
		u.effects.Purchase(receipt)
	}
	return nil
}
