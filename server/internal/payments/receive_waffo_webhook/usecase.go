package receive_waffo_webhook

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct {
	Payload                   []byte
	Signature, CallerIP, Path string
}

// Observer records a received payload only after the SDK configuration has been accepted.
type Observer interface {
	Received(ctx context.Context, input Input)
}

type Usecase struct {
	diagnostics  domain.Diagnostics
	observer     Observer
	clients      domain.WaffoFactory
	transactions domain.Transactions
	completion   domain.Completion
	admission    domain.Admission
}

func NewUsecase(clients domain.WaffoFactory, transactions domain.Transactions, completion domain.Completion, admission domain.Admission, observer Observer, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, clients: clients, transactions: transactions, completion: completion, admission: admission, observer: observer}
}

func (u Usecase) Execute(ctx context.Context, in Input) (domain.WaffoReply, error) {
	client, err := u.clients.Client()
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo webhook SDK initialization failed path=%q client_ip=%s error=%q", in.Path, in.CallerIP, err)
		return domain.WaffoReply{}, domain.ErrPaymentConfig
	}
	u.observer.Received(ctx, in)
	if !client.Verify(in.Payload, in.Signature) {
		u.diagnostics.Warn(ctx, "Waffo webhook signature verification failed path=%q client_ip=%s signature=%q body=%q", in.Path, in.CallerIP, in.Signature, string(in.Payload))
		return domain.WaffoReply{}, domain.ErrWaffoSignature
	}
	event, err := client.Parse(in.Payload)
	if err != nil {
		var payloadError domain.WaffoPayloadError
		if errors.As(err, &payloadError) {
			u.diagnostics.Error(ctx, "Waffo payment notification payload parsing failed event_type=%s client_ip=%s error=%q body=%q", payloadError.EventType, in.CallerIP, payloadError.Cause, string(in.Payload))
		} else {
			u.diagnostics.Error(ctx, "Waffo webhook parsing failed path=%q client_ip=%s error=%q body=%q", in.Path, in.CallerIP, err, string(in.Payload))
		}
		return client.Reply(false, "invalid payload"), nil
	}
	if event.Type != "PAYMENT_NOTIFICATION" {
		u.diagnostics.Info(ctx, "Waffo webhook event ignored event_type=%s client_ip=%s", event.Type, in.CallerIP)
		return client.Reply(true, ""), nil
	}
	u.diagnostics.Info(ctx, "Waffo webhook signature verified and parsed event_type=%s merchant_order_id=%s order_status=%s client_ip=%s", event.Type, event.TradeNo, event.OrderStatus, in.CallerIP)
	if event.OrderStatus != "PAY_SUCCESS" {
		u.diagnostics.Info(ctx, "Waffo order is not successful, ignoring topup trade_no=%s order_status=%s client_ip=%s", event.TradeNo, event.OrderStatus, in.CallerIP)
		if event.TradeNo != "" {
			err := domain.Transition(u.transactions, event.TradeNo, domain.PaymentProviderWaffo, domain.StatusFailed)
			if err != nil && !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrProviderMismatch) && !errors.Is(err, domain.ErrNotPending) {
				u.diagnostics.Error(ctx, "Waffo failed to mark order failed trade_no=%s error=%q", event.TradeNo, err)
			}
		}
		return client.Reply(true, ""), nil
	}
	u.admission.Lock(event.TradeNo)
	defer u.admission.Unlock(event.TradeNo)
	_, err = u.completion.Complete(domain.CompletionInput{TradeNo: event.TradeNo, Provider: domain.PaymentProviderWaffo, CallerIP: in.CallerIP})
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo topup processing failed trade_no=%s client_ip=%s error=%q", event.TradeNo, in.CallerIP, err)
		return client.Reply(false, err.Error()), nil
	}
	u.diagnostics.Info(ctx, "Waffo topup succeeded trade_no=%s client_ip=%s", event.TradeNo, in.CallerIP)
	return client.Reply(true, ""), nil
}
