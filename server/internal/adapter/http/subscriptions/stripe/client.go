package stripe

import (
	shared "github.com/QuantumNous/new-api/internal/adapter/http/payments/stripe"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
)

type Client struct{}

func (Client) Checkout(reference, customer, email, priceID string) (string, error) {
	return shared.Client{}.SubscriptionCheckout(reference, customer, email, priceID, settings.Settings{}.ReturnPath("/wallet"))
}
