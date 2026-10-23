package receive_pancake_webhook

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct{ Payload, Signature, Environment, CallerIP, Path string }

type Usecase struct {
	diagnostics   domain.Diagnostics
	client        domain.PancakeCheckout
	orders        domain.Orders
	subscriptions domain.Subscriptions
	completion    domain.Completion
	admission     domain.Admission
}

func NewUsecase(
	client domain.PancakeCheckout,
	orders domain.Orders,
	subscriptions domain.Subscriptions,
	completion domain.Completion,
	admission domain.Admission, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, client: client, orders: orders, subscriptions: subscriptions, completion: completion, admission: admission}
}

func (u Usecase) Execute(ctx context.Context, in Input) error {
	event, err := u.client.Verify(in.Payload, in.Signature)
	if err != nil {
		u.diagnostics.Warn(ctx, "Waffo Pancake webhook 验签失败 path=%q client_ip=%s signature=%q body=%q error=%q", in.Path, in.CallerIP, in.Signature, in.Payload, err)
		return fmt.Errorf("%w: %v", domain.ErrPancakeSignature, err)
	}
	if !strings.EqualFold(strings.TrimSpace(event.Mode), in.Environment) {
		u.diagnostics.Error(ctx, "Waffo Pancake webhook 环境不匹配 expected=%q actual_mode=%q event_id=%s order_id=%s client_ip=%s", in.Environment, event.Mode, event.ID, event.Data.OrderID, in.CallerIP)
		return nil
	}
	u.diagnostics.Info(ctx, "Waffo Pancake webhook 验签成功 event_type=%s event_id=%s order_id=%s client_ip=%s", event.NormalizedEventType(), event.ID, event.Data.OrderID, in.CallerIP)
	if event.NormalizedEventType() != "order.completed" {
		return nil
	}
	trade := strings.TrimSpace(event.Data.OrderMerchantExternalID)
	subscription := strings.HasPrefix(trade, "WAFFO_PANCAKE_SUB-")
	var order *domain.Order
	var resolveError error
	if trade == "" {
		resolveError = fmt.Errorf("missing webhook orderMerchantExternalId")
	} else {
		if subscription {
			order = u.subscriptions.Find(trade)
		} else {
			order = u.orders.Find(trade)
		}
		kind := ""
		if subscription {
			kind = "subscription "
		}
		if order == nil || order.PaymentProvider != domain.PaymentProviderWaffoPancake {
			resolveError = fmt.Errorf("waffo pancake %sorder not found for tradeNo=%s", kind, trade)
		} else {
			expected := domain.PancakeBuyerIdentity(order.UserID)
			actual := strings.TrimSpace(event.Data.MerchantProvidedBuyerIdentity)
			if actual != expected {
				resolveError = fmt.Errorf("waffo pancake buyer identity mismatch for %stradeNo=%s: expected=%q actual=%q", kind, trade, expected, actual)
			}
		}
	}
	if resolveError != nil {
		if subscription {
			u.diagnostics.Error(ctx, "Waffo Pancake webhook 订阅订单解析失败 event_id=%s order_id=%s buyer_identity=%q client_ip=%s error=%q", event.ID, event.Data.OrderID, event.Data.MerchantProvidedBuyerIdentity, in.CallerIP, resolveError)
		} else {
			u.diagnostics.Error(ctx, "Waffo Pancake webhook 订单解析失败 event_id=%s order_id=%s buyer_identity=%q client_ip=%s error=%q", event.ID, event.Data.OrderID, event.Data.MerchantProvidedBuyerIdentity, in.CallerIP, resolveError)
		}
		return nil
	}
	u.admission.Lock(trade)
	defer u.admission.Unlock(trade)
	if subscription {
		err = u.subscriptions.Complete(trade, in.Payload, domain.PaymentProviderWaffoPancake)
		if err != nil {
			u.diagnostics.Error(ctx, "Waffo Pancake 订阅完成失败 trade_no=%s event_id=%s order_id=%s client_ip=%s error=%q", trade, event.ID, event.Data.OrderID, in.CallerIP, err)
			return err
		}
		u.diagnostics.Info(ctx, "Waffo Pancake 订阅完成 trade_no=%s event_id=%s order_id=%s client_ip=%s", trade, event.ID, event.Data.OrderID, in.CallerIP)
		return nil
	}
	_, err = u.completion.Complete(domain.CompletionInput{TradeNo: trade, Provider: domain.PaymentProviderWaffoPancake})
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo Pancake 充值处理失败 trade_no=%s event_id=%s order_id=%s client_ip=%s error=%q", trade, event.ID, event.Data.OrderID, in.CallerIP, err)
		return err
	}
	u.diagnostics.Info(ctx, "Waffo Pancake 充值成功 trade_no=%s event_id=%s order_id=%s client_ip=%s", trade, event.ID, event.Data.OrderID, in.CallerIP)
	return nil
}
