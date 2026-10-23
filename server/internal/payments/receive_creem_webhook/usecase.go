package receive_creem_webhook

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct {
	Payload                   []byte
	Signature, CallerIP, Path string
}

type Usecase struct {
	diagnostics   domain.Diagnostics
	client        domain.Creem
	orders        domain.Orders
	subscriptions domain.Subscriptions
	completion    domain.Completion
	admission     domain.Admission
}

func NewUsecase(
	client domain.Creem,
	orders domain.Orders,
	subscriptions domain.Subscriptions,
	completion domain.Completion,
	admission domain.Admission, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, client: client, orders: orders, subscriptions: subscriptions, completion: completion, admission: admission}
}

func (u Usecase) Execute(ctx context.Context, in Input) error {
	if in.Signature == "" {
		u.diagnostics.Warn(ctx, "Creem webhook signature is missing path=%q client_ip=%s body_bytes=%d", in.Path, in.CallerIP, len(in.Payload))
		return domain.ErrCreemSignature
	}
	if !u.client.Verify(in.Payload, in.Signature) {
		u.diagnostics.Warn(ctx, "Creem webhook signature verification failed path=%q client_ip=%s body_bytes=%d", in.Path, in.CallerIP, len(in.Payload))
		return domain.ErrCreemSignature
	}
	u.diagnostics.Info(ctx, "Creem webhook signature verified path=%q client_ip=%s", in.Path, in.CallerIP)
	event, err := u.client.Parse(in.Payload)
	if err != nil {
		u.diagnostics.Error(ctx, "Creem webhook parsing failed path=%q client_ip=%s error=%q body_bytes=%d", in.Path, in.CallerIP, err, len(in.Payload))
		return fmt.Errorf("%w: %v", domain.ErrCreemPayload, err)
	}
	u.diagnostics.Info(ctx, "Creem webhook parsed successfully event_type=%s event_id=%s request_id=%s order_id=%s order_status=%s", event.Type, event.ID, event.Reference, event.OrderID, event.Status)
	if event.Type != "checkout.completed" {
		u.diagnostics.Info(ctx, "Creem webhook event ignored event_type=%s event_id=%s", event.Type, event.ID)
		return nil
	}
	if event.Status != "paid" {
		u.diagnostics.Info(ctx, "Creem order is unpaid, ignoring request_id=%s order_id=%s order_status=%s", event.Reference, event.OrderID, event.Status)
		return nil
	}
	if event.Reference == "" {
		u.diagnostics.Warn(ctx, "Creem webhook missing request_id event_id=%s order_id=%s", event.ID, event.OrderID)
		return domain.ErrPaymentTradeNo
	}
	u.admission.Lock(event.Reference)
	defer u.admission.Unlock(event.Reference)
	err = u.subscriptions.Complete(event.Reference, event.SubscriptionPayload, domain.PaymentProviderCreem)
	if err == nil {
		u.diagnostics.Info(ctx, "Creem subscription order processed trade_no=%s creem_order_id=%s", event.Reference, event.OrderID)
		return nil
	}
	if !errors.Is(err, domain.ErrSubscriptionNotFound) {
		u.diagnostics.Error(ctx, "Creem subscription order processing failed trade_no=%s creem_order_id=%s error=%q", event.Reference, event.OrderID, err)
		return err
	}
	if event.OrderType != "onetime" {
		u.diagnostics.Info(ctx, "Creem unsupported order type, ignoring request_id=%s creem_order_id=%s order_type=%s", event.Reference, event.OrderID, event.OrderType)
		return nil
	}
	u.diagnostics.Info(ctx, "Creem payment completion callback trade_no=%s creem_order_id=%s amount_paid=%d currency=%s product_name=%q", event.Reference, event.OrderID, event.AmountPaid, event.Currency, event.ProductName)
	order := u.orders.Find(event.Reference)
	if order == nil {
		u.diagnostics.Warn(ctx, "Creem topup order not found trade_no=%s creem_order_id=%s", event.Reference, event.OrderID)
		return domain.ErrOrderMissing
	}
	if order.Status != domain.StatusPending {
		u.diagnostics.Info(ctx, "Creem topup order status is not pending, ignoring trade_no=%s status=%s creem_order_id=%s", event.Reference, order.Status, event.OrderID)
		return nil
	}
	if event.Email == "" {
		u.diagnostics.Warn(ctx, "Creem callback customer email is empty trade_no=%s creem_order_id=%s", event.Reference, event.OrderID)
	}
	if event.CustomerName == "" {
		u.diagnostics.Warn(ctx, "Creem callback customer name is empty trade_no=%s creem_order_id=%s", event.Reference, event.OrderID)
	}
	_, err = u.completion.Complete(domain.CompletionInput{TradeNo: event.Reference, Provider: domain.PaymentProviderCreem, Email: event.Email, CallerIP: in.CallerIP})
	if err != nil {
		u.diagnostics.Error(ctx, "Creem topup processing failed trade_no=%s creem_order_id=%s client_ip=%s error=%q", event.Reference, event.OrderID, in.CallerIP, err)
		return err
	}
	u.diagnostics.Info(ctx, "Creem topup succeeded trade_no=%s creem_order_id=%s quota=%d money=%.2f client_ip=%s", event.Reference, event.OrderID, order.Amount, order.Money, in.CallerIP)
	return nil
}
