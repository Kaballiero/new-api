package stripe

import (
	"fmt"
	"strings"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhook"

	"github.com/QuantumNous/new-api/common"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/setting"
)

type Client struct{}

func (Client) Checkout(in domain.StripeCheckout) (string, error) {
	referenceId, customerId, email, amount, successURL, cancelURL := in.Reference, in.Customer, in.Email, in.Amount, in.SuccessURL, in.CancelURL
	if !strings.HasPrefix(setting.StripeApiSecret, "sk_") && !strings.HasPrefix(setting.StripeApiSecret, "rk_") {
		return "", fmt.Errorf("invalid Stripe API key")
	}

	stripe.Key = setting.StripeApiSecret

	// Use custom URLs if provided, otherwise use defaults
	if successURL == "" {
		successURL = settings.Settings{}.ReturnPath("/usage-logs")
	}
	if cancelURL == "" {
		cancelURL = settings.Settings{}.ReturnPath("/wallet")
	}

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceId),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(setting.StripePriceId),
				Quantity: stripe.Int64(amount),
			},
		},
		Mode:                stripe.String(string(stripe.CheckoutSessionModePayment)),
		AllowPromotionCodes: stripe.Bool(setting.StripePromotionCodesEnabled),
	}

	if in.Subscription {
		params.LineItems[0].Price = stripe.String(in.PriceID)
		params.LineItems[0].Quantity = stripe.Int64(1)
		params.Mode = stripe.String(string(stripe.CheckoutSessionModeSubscription))
		params.AllowPromotionCodes = nil
	}

	if "" == customerId {
		if "" != email {
			params.CustomerEmail = stripe.String(email)
		}

		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerId)
	}

	result, err := session.New(params)
	if err != nil {
		return "", err
	}

	return result.URL, nil
}

func (Client) Verify(payload []byte, signature string) (domain.StripeEvent, error) {
	event, err := webhook.ConstructEventWithOptions(payload, signature, setting.StripeWebhookSecret, webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		return domain.StripeEvent{}, err
	}
	out := domain.StripeEvent{
		Type:          string(event.Type),
		Reference:     event.GetObjectValue("client_reference_id"),
		Customer:      event.GetObjectValue("customer"),
		Status:        event.GetObjectValue("status"),
		PaymentStatus: event.GetObjectValue("payment_status"),
		AmountTotal:   event.GetObjectValue("amount_total"),
		Currency:      strings.ToUpper(event.GetObjectValue("currency")),
	}
	out.SubscriptionPayload = common.GetJsonString(map[string]any{"customer": out.Customer, "amount_total": out.AmountTotal, "currency": out.Currency, "event_type": out.Type})
	return out, nil
}
