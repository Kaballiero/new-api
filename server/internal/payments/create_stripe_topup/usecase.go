package create_stripe_topup

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct {
	UserID                               int
	Amount                               int64
	PaymentMethod, SuccessURL, CancelURL string
}

type Timing struct {
	Now       func() int64
	Reference func(userID int) string
}

type Usecase struct {
	diagnostics domain.Diagnostics
	pricing     domain.Pricing
	users       domain.Users
	orders      domain.Orders
	client      domain.Stripe
	redirect    func(url string) error
	timing      Timing
}

func NewUsecase(pricing domain.Pricing, users domain.Users, orders domain.Orders, client domain.Stripe, redirect func(url string) error, timing Timing, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, pricing: pricing, users: users, orders: orders, client: client, redirect: redirect, timing: timing}
}

func (u Usecase) Execute(ctx context.Context, in Input) (string, error) {
	if in.PaymentMethod != domain.PaymentMethodStripe {
		return "", domain.ErrChannel
	}
	if in.Amount < u.pricing.Minimum(domain.PaymentProviderStripe) {
		return "", domain.MinimumAmount{Amount: u.pricing.Minimum(domain.PaymentProviderStripe)}
	}
	if in.Amount > 10000 {
		return "", domain.ErrStripeMaximum
	}
	if in.SuccessURL != "" && u.redirect(in.SuccessURL) != nil {
		return "", domain.ErrSuccessURL
	}
	if in.CancelURL != "" && u.redirect(in.CancelURL) != nil {
		return "", domain.ErrCancelURL
	}
	user, err := u.users.Find(in.UserID)
	if err != nil || user == nil {
		return "", domain.ErrUser
	}
	money := float64(in.Amount) * u.pricing.Ratio(user.Group)
	if err := u.pricing.CheckCredited(in.UserID, decimal.NewFromFloat(money).Mul(decimal.NewFromFloat(u.pricing.Settings.QuotaPerUnit()))); err != nil {
		return "", err
	}
	reference := u.timing.Reference(user.ID)
	link, err := u.client.Checkout(
		domain.StripeCheckout{
			Reference:  reference,
			Customer:   user.StripeCustomer,
			Email:      user.Email,
			Amount:     in.Amount,
			SuccessURL: in.SuccessURL,
			CancelURL:  in.CancelURL,
		},
	)
	if err != nil {
		u.diagnostics.Error(ctx, "Stripe create Checkout Session failed user_id=%d trade_no=%s amount=%d error=%q", in.UserID, reference, in.Amount, err)
		return "", domain.ErrCheckout
	}
	order := domain.Order{
		UserID:          in.UserID,
		Amount:          in.Amount,
		Money:           money,
		TradeNo:         reference,
		PaymentMethod:   domain.PaymentMethodStripe,
		PaymentProvider: domain.PaymentProviderStripe,
		CreateTime:      u.timing.Now(),
		Status:          domain.StatusPending,
	}
	if err := u.orders.Insert(&order); err != nil {
		u.diagnostics.Error(ctx, "Stripe topup order creation failed user_id=%d trade_no=%s amount=%d error=%q", in.UserID, reference, in.Amount, err)
		return "", domain.ErrInsert
	}
	u.diagnostics.Info(ctx, "Stripe topup order created user_id=%d trade_no=%s amount=%d money=%.2f", in.UserID, reference, in.Amount, money)
	return link, nil
}
