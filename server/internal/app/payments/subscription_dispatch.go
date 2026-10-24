package payments

import (
	"errors"

	subscriptionsapp "github.com/QuantumNous/new-api/internal/app/subscriptions"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/complete_order"
	subscriptionsdomain "github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

// subscriptionDispatch implements the payments consumer port with the subscriptions owner.
type subscriptionDispatch struct{ orders subscriptionsapp.Orders }

func (s subscriptionDispatch) Complete(tradeNo, payload, provider string) error {
	return subscriptionError(s.orders.Complete(complete_order.Input{TradeNo: tradeNo, ProviderPayload: payload, ExpectedProvider: provider}))
}

func (s subscriptionDispatch) Expire(tradeNo, provider string) error {
	return subscriptionError(s.orders.Expire(tradeNo, provider))
}

func (s subscriptionDispatch) Find(tradeNo string) *domain.Order {
	order := s.orders.Find(tradeNo)
	if order == nil {
		return nil
	}
	return &domain.Order{UserID: order.UserId, TradeNo: order.TradeNo, PaymentProvider: order.PaymentProvider, Status: order.Status}
}

func subscriptionError(err error) error {
	if errors.Is(err, subscriptionsdomain.ErrOrderNotFound) {
		return domain.ErrSubscriptionNotFound
	}
	if errors.Is(err, subscriptionsdomain.ErrProviderMismatch) {
		return domain.ErrProviderMismatch
	}
	return err
}
