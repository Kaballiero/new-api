package stripe

import (
	shared "github.com/QuantumNous/new-api/internal/adapter/http/payments/stripe"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Client struct{}

func (Client) Checkout(reference, customer, email, priceID string) (string, error) {
	return shared.Client{}.Checkout(domain.StripeCheckout{Reference: reference, Customer: customer, Email: email, PriceID: priceID, Subscription: true, SuccessURL: settings.Settings{}.ReturnPath("/wallet"), CancelURL: settings.Settings{}.ReturnPath("/wallet")})
}
