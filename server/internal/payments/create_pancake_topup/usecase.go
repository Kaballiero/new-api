package create_pancake_topup

import (
	"context"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

// Settings supplies the configured product for each checkout.
type Settings interface{ PancakeProductID() string }

type Input struct {
	UserID int
	Amount int64
}

type Result struct {
	Session domain.WaffoPancakeCheckoutSession
	OrderID string
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
	client      domain.PancakeCheckout
	settings    Settings
	timing      Timing
}

func NewUsecase(pricing domain.Pricing, users domain.Users, orders domain.Orders, client domain.PancakeCheckout, settings Settings, timing Timing, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, pricing: pricing, users: users, orders: orders, client: client, settings: settings, timing: timing}
}

func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	minimum := u.pricing.Settings.MinTopUp(domain.PaymentProviderWaffoPancake)
	if in.Amount < minimum {
		return Result{}, domain.MinimumAmount{Amount: minimum}
	}
	if err := u.pricing.Check(in.UserID, in.Amount); err != nil {
		return Result{}, err
	}
	user, err := u.users.Find(in.UserID)
	if err != nil || user == nil {
		return Result{}, domain.ErrUser
	}
	group, err := u.users.Group(in.UserID)
	if err != nil {
		return Result{}, domain.ErrGroup
	}
	money := u.pricing.DecimalMoney(in.Amount, group, domain.PaymentProviderWaffoPancake)
	if money < 0.01 {
		return Result{}, domain.ErrLowAmount
	}
	trade := u.timing.Reference(in.UserID)
	amount := u.pricing.StoredAmount(in.Amount)
	if u.pricing.Settings.TokenDisplay() && amount < 1 {
		amount = 1
	}
	order := domain.Order{
		UserID:          in.UserID,
		Amount:          amount,
		Money:           money,
		TradeNo:         trade,
		PaymentMethod:   domain.PaymentMethodWaffoPancake,
		PaymentProvider: domain.PaymentProviderWaffoPancake,
		CreateTime:      u.timing.Now(),
		Status:          domain.StatusPending,
	}
	if err := u.orders.Insert(&order); err != nil {
		u.diagnostics.Error(ctx, "Waffo Pancake 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", in.UserID, trade, in.Amount, err)
		return Result{}, domain.ErrInsert
	}
	expires := 45 * 60
	session, err := domain.CreatePancakeCheckout(
		ctx,
		u.client,
		&domain.WaffoPancakeCreateSessionParams{
			ProductID:               u.settings.PancakeProductID(),
			BuyerIdentity:           domain.PancakeBuyerIdentity(user.ID),
			PriceSnapshot:           &domain.WaffoPancakePriceSnapshot{Amount: domain.PancakeAmount(money), TaxCategory: "saas"},
			BuyerEmail:              domain.PancakeBuyerEmail(user.Email),
			ExpiresInSeconds:        &expires,
			OrderMerchantExternalID: trade,
		},
	)
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo Pancake 创建结账会话失败 user_id=%d trade_no=%s error=%q", in.UserID, trade, err)
		order.Status = domain.StatusFailed
		_ = u.orders.Save(&order)
		return Result{}, domain.ErrCheckout
	}
	u.diagnostics.Info(ctx, "Waffo Pancake 充值订单创建成功 user_id=%d trade_no=%s session_id=%s amount=%d money=%.2f", in.UserID, trade, session.SessionID, in.Amount, money)
	return Result{Session: *session, OrderID: trade}, nil
}
