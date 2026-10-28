package stripe

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/setting"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhook"
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

	return createSession(params, customerId, email)
}

// SubscriptionCheckout uses the plan price with one recurring item and wallet redirects.
func (Client) SubscriptionCheckout(reference, customer, email, priceID, returnURL string) (string, error) {
	if !strings.HasPrefix(setting.StripeApiSecret, "sk_") && !strings.HasPrefix(setting.StripeApiSecret, "rk_") {
		return "", fmt.Errorf("invalid Stripe API key")
	}
	stripe.Key = setting.StripeApiSecret
	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(reference),
		SuccessURL:        stripe.String(returnURL),
		CancelURL:         stripe.String(returnURL),
		LineItems:         []*stripe.CheckoutSessionLineItemParams{{Price: stripe.String(priceID), Quantity: stripe.Int64(1)}},
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
	}
	return createSession(params, customer, email)
}

func createSession(params *stripe.CheckoutSessionParams, customerId, email string) (string, error) {
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
