package create_waffo_topup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

// Settings reads method definitions and provider checkout addresses at their original call sites.
type Settings interface {
	Methods() []domain.PayMethod
	CallbackAddress() string
	ReturnPath(suffix string) string
	WaffoNotifyURL() string
	WaffoReturnURL() string
	WaffoCurrency() string
	WaffoMerchantID() string
	AppName() string
}

type Input struct {
	UserID                 int
	Amount                 int64
	Index                  *int
	MethodType, MethodName string
}

type Result struct{ PaymentURL, OrderID string }

type Timing struct {
	Now         func() int64
	Reference   func(userID int) string
	RequestedAt func() string
}

type Usecase struct {
	diagnostics domain.Diagnostics
	pricing     domain.Pricing
	users       domain.Users
	orders      domain.Orders
	clients     domain.WaffoFactory
	settings    Settings
	timing      Timing
}

func NewUsecase(pricing domain.Pricing, users domain.Users, orders domain.Orders, clients domain.WaffoFactory, settings Settings, timing Timing, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, pricing: pricing, users: users, orders: orders, clients: clients, settings: settings, timing: timing}
}

func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	minimum := u.pricing.Settings.MinTopUp(domain.PaymentProviderWaffo)
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
	methods := u.settings.Methods()
	kind, name, err := domain.ResolveMethod(in.Index, in.MethodType, in.MethodName, methods)
	if err != nil {
		if in.Index != nil {
			u.diagnostics.Warn(ctx, "Waffo payment method index is invalid user_id=%d pay_method_index=%d method_count=%d", in.UserID, *in.Index, len(methods))
		} else {
			u.diagnostics.Warn(ctx, "Waffo payment method is invalid user_id=%d pay_method_type=%s pay_method_name=%q", in.UserID, in.MethodType, in.MethodName)
		}
		return Result{}, err
	}
	group, _ := u.users.Group(in.UserID)
	money := u.pricing.FloatMoney(float64(in.Amount), group, domain.PaymentProviderWaffo)
	if money < 0.01 {
		return Result{}, domain.ErrLowAmount
	}
	trade := u.timing.Reference(in.UserID)
	amount := in.Amount
	if u.pricing.Settings.TokenDisplay() {
		amount = max(int64(float64(in.Amount)/u.pricing.Settings.QuotaPerUnit()), 1)
	}
	order := domain.Order{
		UserID:          in.UserID,
		Amount:          amount,
		Money:           money,
		TradeNo:         trade,
		PaymentMethod:   domain.PaymentMethodWaffo,
		PaymentProvider: domain.PaymentProviderWaffo,
		CreateTime:      u.timing.Now(),
		Status:          domain.StatusPending,
	}
	if err := u.orders.Insert(&order); err != nil {
		u.diagnostics.Error(ctx, "Waffo topup order creation failed user_id=%d trade_no=%s amount=%d error=%q", in.UserID, trade, in.Amount, err)
		return Result{}, domain.ErrInsert
	}
	client, err := u.clients.Client()
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo SDK initialization failed user_id=%d trade_no=%s error=%q", in.UserID, trade, err)
		order.Status = domain.StatusFailed
		_ = u.orders.Save(&order)
		return Result{}, domain.ErrPaymentConfig
	}
	notify := u.settings.CallbackAddress() + "/api/waffo/webhook"
	if u.settings.WaffoNotifyURL() != "" {
		notify = u.settings.WaffoNotifyURL()
	}
	back := u.settings.ReturnPath("/wallet?show_history=true")
	if u.settings.WaffoReturnURL() != "" {
		back = u.settings.WaffoReturnURL()
	}
	currency := u.settings.WaffoCurrency()
	if currency == "" {
		currency = "USD"
	}
	appName := strings.TrimSpace(u.settings.AppName())
	if appName == "" {
		appName = "New API"
	}
	link, err := client.Checkout(
		ctx,
		domain.WaffoCheckout{
			TradeNo:     trade,
			Money:       domain.WaffoMoney(money, currency),
			Currency:    currency,
			Description: fmt.Sprintf("Recharge %d credits", in.Amount),
			AppName:     appName,
			RequestedAt: u.timing.RequestedAt(),
			NotifyURL:   notify,
			ReturnURL:   back,
			MerchantID:  u.settings.WaffoMerchantID(),
			UserID:      user.ID,
			MethodType:  kind,
			MethodName:  name,
		},
	)
	if err != nil {
		var rejection domain.WaffoRejection
		if errors.As(err, &rejection) {
			u.diagnostics.Warn(ctx, "Waffo order creation rejected user_id=%d trade_no=%s code=%s message=%q response=%q", in.UserID, trade, rejection.Code, rejection.Message, rejection.Response)
		} else {
			u.diagnostics.Error(ctx, "Waffo order creation failed user_id=%d trade_no=%s error=%q", in.UserID, trade, err)
		}
		order.Status = domain.StatusFailed
		_ = u.orders.Save(&order)
		return Result{}, domain.ErrCheckout
	}
	u.diagnostics.Info(ctx, "Waffo topup order created user_id=%d trade_no=%s amount=%d money=%.2f pay_method_type=%s pay_method_name=%q", in.UserID, trade, in.Amount, money, kind, name)
	return Result{PaymentURL: link, OrderID: trade}, nil
}
