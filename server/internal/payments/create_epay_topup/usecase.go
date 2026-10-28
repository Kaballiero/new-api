package create_epay_topup

import (
	"context"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

// Settings supplies allowed methods and callback/return addresses without normalizing stored settings.
type Settings interface {
	ContainsMethod(method string) bool
	CallbackAddress() string
	ReturnPath(suffix string) string
}

type Input struct {
	UserID        int
	Amount        int64
	PaymentMethod string
}

type Result struct {
	URL     string
	Params  map[string]string
	TradeNo string
	Money   float64
}

type Timing struct {
	Now     func() int64
	TradeNo func(userID int) string
}

type Usecase struct {
	diagnostics domain.Diagnostics
	pricing     domain.Pricing
	users       domain.Users
	orders      domain.Orders
	settings    Settings
	clients     domain.EpayFactory
	timing      Timing
}

func NewUsecase(pricing domain.Pricing, users domain.Users, orders domain.Orders, settings Settings, clients domain.EpayFactory, timing Timing, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, pricing: pricing, users: users, orders: orders, settings: settings, clients: clients, timing: timing}
}

func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	if in.Amount < u.pricing.Minimum(domain.PaymentProviderEpay) {
		return Result{}, domain.MinimumAmount{Amount: u.pricing.Minimum(domain.PaymentProviderEpay)}
	}
	if err := u.pricing.Check(in.UserID, in.Amount); err != nil {
		return Result{}, err
	}
	group, err := u.users.Group(in.UserID)
	if err != nil {
		return Result{}, domain.ErrGroup
	}
	money := u.pricing.DecimalMoney(in.Amount, group, domain.PaymentProviderEpay)
	if money < 0.01 {
		return Result{}, domain.ErrLowAmount
	}
	if !u.settings.ContainsMethod(in.PaymentMethod) {
		return Result{}, domain.ErrMethod
	}
	callback := u.settings.CallbackAddress()
	back := u.settings.ReturnPath("/usage-logs")
	trade := u.timing.TradeNo(in.UserID)
	client := u.clients.Client()
	if client == nil {
		return Result{}, domain.ErrEpayConfig
	}
	uri, params, err := client.Purchase(
		domain.EpayPurchase{
			TradeNo:   trade,
			Method:    in.PaymentMethod,
			Name:      fmt.Sprintf("TUC%d", in.Amount),
			Money:     strconv.FormatFloat(money, 'f', 2, 64),
			NotifyURL: callback + "/api/user/epay/notify",
			ReturnURL: back,
		},
	)
	if err != nil {
		u.diagnostics.Error(ctx, "Epay payment checkout failed user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", in.UserID, trade, in.PaymentMethod, in.Amount, err)
		return Result{}, domain.ErrCheckout
	}
	order := domain.Order{
		UserID:          in.UserID,
		Amount:          u.pricing.StoredAmount(in.Amount),
		Money:           money,
		TradeNo:         trade,
		PaymentMethod:   in.PaymentMethod,
		PaymentProvider: domain.PaymentProviderEpay,
		CreateTime:      u.timing.Now(),
		Status:          domain.StatusPending,
	}
	if err := u.orders.Insert(&order); err != nil {
		u.diagnostics.Error(ctx, "Epay topup order creation failed user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", in.UserID, trade, in.PaymentMethod, in.Amount, err)
		return Result{}, domain.ErrInsert
	}
	return Result{URL: uri, Params: params, TradeNo: trade, Money: money}, nil
}
