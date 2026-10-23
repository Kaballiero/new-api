package receive_stripe_webhook

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct {
	Payload                   []byte
	Signature, CallerIP, Path string
}

type Usecase struct {
	client        domain.Stripe
	orders        domain.Orders
	transactions  domain.Transactions
	subscriptions domain.Subscriptions
	completion    domain.Completion
	admission     domain.Admission
	diagnostics   domain.Diagnostics
}

func NewUsecase(client domain.Stripe, orders domain.Orders, transactions domain.Transactions, subscriptions domain.Subscriptions, completion domain.Completion, admission domain.Admission, diagnostics domain.Diagnostics) Usecase {
	return Usecase{client: client, orders: orders, transactions: transactions, subscriptions: subscriptions, completion: completion, admission: admission, diagnostics: diagnostics}
}

func (u Usecase) Execute(ctx context.Context, in Input) error {
	event, err := u.client.Verify(in.Payload, in.Signature)
	if err != nil {
		u.diagnostics.Warn(ctx, "Stripe webhook 验签失败 path=%q client_ip=%s error=%q", in.Path, in.CallerIP, err)
		return fmt.Errorf("%w: %v", domain.ErrStripeSignature, err)
	}
	u.diagnostics.Info(ctx, "Stripe webhook 验签成功 event_type=%s client_ip=%s path=%q", event.Type, in.CallerIP, in.Path)
	switch event.Type {
	case "checkout.session.completed":
		if event.Status != "complete" {
			u.diagnostics.Warn(ctx, "Stripe checkout.completed 状态异常，忽略处理 trade_no=%s status=%s client_ip=%s", event.Reference, event.Status, in.CallerIP)
			return nil
		}
		if event.PaymentStatus != "paid" {
			u.diagnostics.Info(ctx, "Stripe Checkout 支付未完成，等待异步结果 trade_no=%s payment_status=%s client_ip=%s", event.Reference, event.PaymentStatus, in.CallerIP)
			return nil
		}
	case "checkout.session.async_payment_succeeded":
		u.diagnostics.Info(ctx, "Stripe 异步支付成功 trade_no=%s client_ip=%s", event.Reference, in.CallerIP)
	case "checkout.session.expired":
		if event.Status != "expired" {
			u.diagnostics.Warn(ctx, "Stripe checkout.expired 状态异常，忽略处理 trade_no=%s status=%s", event.Reference, event.Status)
			return nil
		}
	case "checkout.session.async_payment_failed":
		u.diagnostics.Warn(ctx, "Stripe 异步支付失败 trade_no=%s client_ip=%s", event.Reference, in.CallerIP)
	default:
		u.diagnostics.Info(ctx, "Stripe webhook 忽略事件 event_type=%s client_ip=%s", event.Type, in.CallerIP)
		return nil
	}
	if event.Reference == "" {
		switch event.Type {
		case "checkout.session.async_payment_failed":
			u.diagnostics.Warn(ctx, "Stripe 异步支付失败事件缺少订单号 client_ip=%s", in.CallerIP)
		case "checkout.session.expired":
			u.diagnostics.Warn(ctx, "Stripe checkout.expired 缺少订单号")
		default:
			u.diagnostics.Warn(ctx, "Stripe 完成订单时缺少订单号 client_ip=%s", in.CallerIP)
		}
		return nil
	}
	u.admission.Lock(event.Reference)
	defer u.admission.Unlock(event.Reference)
	switch event.Type {
	case "checkout.session.async_payment_failed":
		order := u.orders.Find(event.Reference)
		if order == nil {
			u.diagnostics.Warn(ctx, "Stripe 异步支付失败但本地订单不存在 trade_no=%s client_ip=%s", event.Reference, in.CallerIP)
			return nil
		}
		if order.PaymentProvider != domain.PaymentProviderStripe {
			u.diagnostics.Warn(ctx, "Stripe 异步支付失败但订单支付网关不匹配 trade_no=%s payment_provider=%s client_ip=%s", event.Reference, order.PaymentProvider, in.CallerIP)
			return nil
		}
		if order.Status != domain.StatusPending {
			u.diagnostics.Info(ctx, "Stripe 异步支付失败但订单状态非 pending，忽略处理 trade_no=%s status=%s client_ip=%s", event.Reference, order.Status, in.CallerIP)
			return nil
		}
		order.Status = domain.StatusFailed
		if err := u.orders.Save(order); err != nil {
			u.diagnostics.Error(ctx, "Stripe 标记充值订单失败状态失败 trade_no=%s client_ip=%s error=%q", event.Reference, in.CallerIP, err)
			return nil
		}
		u.diagnostics.Info(ctx, "Stripe 充值订单已标记为失败 trade_no=%s client_ip=%s", event.Reference, in.CallerIP)
	case "checkout.session.expired":
		err := u.subscriptions.Expire(event.Reference, domain.PaymentProviderStripe)
		if err == nil {
			u.diagnostics.Info(ctx, "Stripe 订阅订单已过期 trade_no=%s", event.Reference)
			return nil
		}
		if !errors.Is(err, domain.ErrSubscriptionNotFound) {
			u.diagnostics.Error(ctx, "Stripe 订阅订单过期处理失败 trade_no=%s error=%q", event.Reference, err)
			return nil
		}
		err = domain.Transition(u.transactions, event.Reference, domain.PaymentProviderStripe, domain.StatusExpired)
		if errors.Is(err, domain.ErrNotFound) {
			u.diagnostics.Warn(ctx, "Stripe 充值订单不存在，无法标记过期 trade_no=%s", event.Reference)
			return nil
		}
		if err != nil {
			u.diagnostics.Error(ctx, "Stripe 充值订单过期处理失败 trade_no=%s error=%q", event.Reference, err)
			return nil
		}
		u.diagnostics.Info(ctx, "Stripe 充值订单已过期 trade_no=%s", event.Reference)
	default:
		err := u.subscriptions.Complete(event.Reference, event.SubscriptionPayload, domain.PaymentProviderStripe)
		if err == nil {
			u.diagnostics.Info(ctx, "Stripe 订阅订单处理成功 trade_no=%s event_type=%s client_ip=%s", event.Reference, event.Type, in.CallerIP)
			return nil
		}
		if !errors.Is(err, domain.ErrSubscriptionNotFound) {
			u.diagnostics.Error(ctx, "Stripe 订阅订单处理失败 trade_no=%s event_type=%s client_ip=%s error=%q", event.Reference, event.Type, in.CallerIP, err)
			return nil
		}
		_, err = u.completion.Complete(domain.CompletionInput{TradeNo: event.Reference, Provider: domain.PaymentProviderStripe, Customer: event.Customer, CallerIP: in.CallerIP})
		if err != nil {
			u.diagnostics.Error(ctx, "Stripe 充值处理失败 trade_no=%s event_type=%s client_ip=%s error=%q", event.Reference, event.Type, in.CallerIP, err)
			return nil
		}
		total, _ := strconv.ParseFloat(event.AmountTotal, 64)
		u.diagnostics.Info(ctx, "Stripe 充值成功 trade_no=%s amount_total=%.2f currency=%s event_type=%s client_ip=%s", event.Reference, total/100, event.Currency, event.Type, in.CallerIP)
	}
	// Stripe acknowledges verified dispatch even when completion or persistence failed.
	return nil
}
