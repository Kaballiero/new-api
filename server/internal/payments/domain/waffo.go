package domain

import (
	"context"
	"fmt"
)

type PayMethod struct{ Name, Icon, PayMethodType, PayMethodName string }

type WaffoCheckout struct {
	TradeNo, Money, Currency, Description, AppName, RequestedAt, NotifyURL, ReturnURL, MerchantID string
	UserID                                                                                        int
	MethodType, MethodName                                                                        string
}

type WaffoEvent struct{ Type, TradeNo, OrderStatus string }

// WaffoRejection distinguishes a verified business rejection from a transport error.
type WaffoRejection struct{ Code, Message, Response string }

func (e WaffoRejection) Error() string {
	return fmt.Sprintf("Waffo rejected checkout: %s: %s", e.Code, e.Message)
}

// WaffoPayloadError identifies the notification stage without exposing SDK types.
type WaffoPayloadError struct {
	EventType string
	Cause     error
}

func (e WaffoPayloadError) Error() string { return e.Cause.Error() }

type WaffoReply struct{ Body, Signature string }

// WaffoClient serializes and verifies the provider protocol using one configured SDK instance.
type WaffoClient interface {
	Checkout(ctx context.Context, checkout WaffoCheckout) (string, error)
	Verify(payload []byte, signature string) bool
	Parse(payload []byte) (WaffoEvent, error)
	Reply(success bool, message string) WaffoReply
}

// WaffoFactory builds the active environment SDK without admitting a payment.
type WaffoFactory interface{ Client() (WaffoClient, error) }

func WaffoMoney(amount float64, currency string) string {
	switch currency {
	case "IDR", "JPY", "KRW", "VND":
		return fmt.Sprintf("%.0f", amount)
	}
	return fmt.Sprintf("%.2f", amount)
}

func ResolveMethod(index *int, kind, name string, methods []PayMethod) (string, string, error) {
	if index != nil {
		if *index < 0 || *index >= len(methods) {
			return "", "", ErrPayMethod
		}
		return methods[*index].PayMethodType, methods[*index].PayMethodName, nil
	}
	if kind != "" {
		for _, method := range methods {
			if method.PayMethodType == kind && method.PayMethodName == name {
				return method.PayMethodType, method.PayMethodName, nil
			}
		}
		return "", "", ErrPayMethod
	}
	return "", "", nil
}
