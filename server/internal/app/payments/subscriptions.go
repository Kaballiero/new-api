package payments

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/model"
)

// subscriptions is the explicit legacy boundary; webhook dispatch stays in payments.
type subscriptions struct{}

func (subscriptions) Complete(tradeNo, payload, provider string) error {
	return subscriptionError(model.CompleteSubscriptionOrder(tradeNo, payload, provider, ""))
}
func (subscriptions) Expire(tradeNo, provider string) error {
	return subscriptionError(model.ExpireSubscriptionOrder(tradeNo, provider))
}
func (subscriptions) Find(tradeNo string) *domain.Order {
	order := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if order == nil {
		return nil
	}
	return &domain.Order{UserID: order.UserId, TradeNo: order.TradeNo, PaymentProvider: order.PaymentProvider, Status: order.Status}
}
func subscriptionError(err error) error {
	if errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		return domain.ErrSubscriptionNotFound
	}
	if errors.Is(err, model.ErrPaymentMethodMismatch) {
		return domain.ErrProviderMismatch
	}
	return err
}
