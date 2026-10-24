package create_stripe_order

import (
	"context"
	"strings"

	payments "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

type Diagnostics interface {
	Error(context.Context, string, ...any)
}

type Input struct{ UserID, PlanID int }

type Store interface {
	PlanByID(int) (*domain.SubscriptionPlan, error)
	CountPurchases(int, int) (int64, error)
	InsertOrder(*domain.SubscriptionOrder) error
	UserByID(int) (*domain.CheckoutUser, error)
}
type Settings interface{ Availability() payments.Availability }

type Client interface {
	Checkout(reference, customer, email, priceID string) (string, error)
}
type Usecase struct {
	store                     Store
	settings                  Settings
	client                    Client
	diagnostics               Diagnostics
	now                       func() int64
	reference                 func(int) string
	pending, method, provider string
}

func NewUsecase(
	store Store,
	settings Settings,
	client Client,
	diagnostics Diagnostics,
	now func() int64,
	reference func(int) string,
	pending, method, provider string,
) Usecase {
	return Usecase{store, settings, client, diagnostics, now, reference, pending, method, provider}
}
func (u Usecase) Execute(ctx context.Context, in Input) (string, error) {
	userID, planID := in.UserID, in.PlanID
	plan, err := u.store.PlanByID(planID)
	if err != nil {
		return "", err
	}
	if !plan.Enabled {
		return "", domain.ErrPlanDisabled
	}
	if plan.StripePriceId == "" {
		return "", domain.ErrStripePriceMissing
	}
	a := u.settings.Availability()
	if !strings.HasPrefix(a.StripeAPI, "sk_") && !strings.HasPrefix(a.StripeAPI, "rk_") {
		return "", domain.ErrStripeConfig
	}
	if a.StripeWebhook == "" {
		return "", domain.ErrStripeWebhook
	}
	user, err := u.store.UserByID(userID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", domain.ErrUserMissing
	}
	if plan.MaxPurchasePerUser > 0 {
		count, err := u.store.CountPurchases(userID, plan.Id)
		if err != nil {
			return "", err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return "", domain.ErrPurchaseLimit
		}
	}
	ref := u.reference(user.ID)
	link, err := u.client.Checkout(ref, user.StripeCustomer, user.Email, plan.StripePriceId)
	if err != nil {
		u.diagnostics.Error(ctx, "Stripe 订阅支付链接创建失败 trade_no=%s plan_id=%d error=%q", ref, plan.Id, err.Error())
		return "", domain.ErrCheckout
	}
	order := domain.SubscriptionOrder{
		UserId:          userID,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         ref,
		PaymentMethod:   u.method,
		PaymentProvider: u.provider,
		CreateTime:      u.now(),
		Status:          u.pending,
	}
	if err := u.store.InsertOrder(&order); err != nil {
		return "", domain.ErrCreateOrder
	}
	return link, nil
}
