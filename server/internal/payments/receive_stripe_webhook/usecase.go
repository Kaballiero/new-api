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
		u.diagnostics.Warn(ctx, "Stripe webhook signature verification failed path=%q client_ip=%s error=%q", in.Path, in.CallerIP, err)
		return fmt.Errorf("%w: %v", domain.ErrStripeSignature, err)
	}
	u.diagnostics.Info(ctx, "Stripe webhook signature verified event_type=%s client_ip=%s path=%q", event.Type, in.CallerIP, in.Path)
	switch event.Type {
	case "checkout.session.completed":
		if event.Status != "complete" {
			u.diagnostics.Warn(ctx, "Stripe checkout.completed unexpected status, ignoring trade_no=%s status=%s client_ip=%s", event.Reference, event.Status, in.CallerIP)
			return nil
		}
		if event.PaymentStatus != "paid" {
			u.diagnostics.Info(ctx, "Stripe Checkout payment is incomplete, waiting for asynchronous result trade_no=%s payment_status=%s client_ip=%s", event.Reference, event.PaymentStatus, in.CallerIP)
			return nil
		}
	case "checkout.session.async_payment_succeeded":
		u.diagnostics.Info(ctx, "Stripe asynchronous payment succeeded trade_no=%s client_ip=%s", event.Reference, in.CallerIP)
	case "checkout.session.expired":
		if event.Status != "expired" {
			u.diagnostics.Warn(ctx, "Stripe checkout.expired unexpected status, ignoring trade_no=%s status=%s", event.Reference, event.Status)
			return nil
		}
	case "checkout.session.async_payment_failed":
		u.diagnostics.Warn(ctx, "Stripe asynchronous payment failed trade_no=%s client_ip=%s", event.Reference, in.CallerIP)
	default:
		u.diagnostics.Info(ctx, "Stripe webhook event ignored event_type=%s client_ip=%s", event.Type, in.CallerIP)
		return nil
	}
	if event.Reference == "" {
		switch event.Type {
		case "checkout.session.async_payment_failed":
			u.diagnostics.Warn(ctx, "Stripe asynchronous payment failure event is missing order number client_ip=%s", in.CallerIP)
		case "checkout.session.expired":
			u.diagnostics.Warn(ctx, "Stripe checkout.expired order number is missing")
		default:
			u.diagnostics.Warn(ctx, "Stripe missing order number during completion client_ip=%s", in.CallerIP)
		}
		return nil
	}
	u.admission.Lock(event.Reference)
	defer u.admission.Unlock(event.Reference)
	switch event.Type {
	case "checkout.session.async_payment_failed":
		order := u.orders.Find(event.Reference)
		if order == nil {
			u.diagnostics.Warn(ctx, "Stripe asynchronous payment failed but local order was not found trade_no=%s client_ip=%s", event.Reference, in.CallerIP)
			return nil
		}
		if order.PaymentProvider != domain.PaymentProviderStripe {
			u.diagnostics.Warn(ctx, "Stripe asynchronous payment failed but order provider mismatches trade_no=%s payment_provider=%s client_ip=%s", event.Reference, order.PaymentProvider, in.CallerIP)
			return nil
		}
		if order.Status != domain.StatusPending {
			u.diagnostics.Info(ctx, "Stripe asynchronous payment failed but order status is not pending, ignoring trade_no=%s status=%s client_ip=%s", event.Reference, order.Status, in.CallerIP)
			return nil
		}
		order.Status = domain.StatusFailed
		if err := u.orders.Save(order); err != nil {
			u.diagnostics.Error(ctx, "Stripe failed to mark topup order failed trade_no=%s client_ip=%s error=%q", event.Reference, in.CallerIP, err)
			return nil
		}
		u.diagnostics.Info(ctx, "Stripe topup order marked failed trade_no=%s client_ip=%s", event.Reference, in.CallerIP)
	case "checkout.session.expired":
		err := u.subscriptions.Expire(event.Reference, domain.PaymentProviderStripe)
		if err == nil {
			u.diagnostics.Info(ctx, "Stripe subscription order expired trade_no=%s", event.Reference)
			return nil
		}
		if !errors.Is(err, domain.ErrSubscriptionNotFound) {
			u.diagnostics.Error(ctx, "Stripe subscription order expiry failed trade_no=%s error=%q", event.Reference, err)
			return nil
		}
		err = domain.Transition(u.transactions, event.Reference, domain.PaymentProviderStripe, domain.StatusExpired)
		if errors.Is(err, domain.ErrNotFound) {
			u.diagnostics.Warn(ctx, "Stripe topup order not found, cannot mark expired trade_no=%s", event.Reference)
			return nil
		}
		if err != nil {
			u.diagnostics.Error(ctx, "Stripe topup order expiry failed trade_no=%s error=%q", event.Reference, err)
			return nil
		}
		u.diagnostics.Info(ctx, "Stripe topup order expired trade_no=%s", event.Reference)
	default:
		err := u.subscriptions.Complete(event.Reference, event.SubscriptionPayload, domain.PaymentProviderStripe)
		if err == nil {
			u.diagnostics.Info(ctx, "Stripe subscription order processed trade_no=%s event_type=%s client_ip=%s", event.Reference, event.Type, in.CallerIP)
			return nil
		}
		if !errors.Is(err, domain.ErrSubscriptionNotFound) {
			u.diagnostics.Error(ctx, "Stripe subscription order processing failed trade_no=%s event_type=%s client_ip=%s error=%q", event.Reference, event.Type, in.CallerIP, err)
			return nil
		}
		_, err = u.completion.Complete(domain.CompletionInput{TradeNo: event.Reference, Provider: domain.PaymentProviderStripe, Customer: event.Customer, CallerIP: in.CallerIP})
		if err != nil {
			u.diagnostics.Error(ctx, "Stripe topup processing failed trade_no=%s event_type=%s client_ip=%s error=%q", event.Reference, event.Type, in.CallerIP, err)
			return nil
		}
		total, _ := strconv.ParseFloat(event.AmountTotal, 64)
		u.diagnostics.Info(ctx, "Stripe topup succeeded trade_no=%s amount_total=%.2f currency=%s event_type=%s client_ip=%s", event.Reference, total/100, event.Currency, event.Type, in.CallerIP)
	}
	// Stripe acknowledges verified dispatch even when completion or persistence failed.
	return nil
}
