package domain

type StripeCheckout struct {
	Subscription                                      bool
	PriceID                                           string
	Reference, Customer, Email, SuccessURL, CancelURL string
	Amount                                            int64
}

type StripeEvent struct{ Type, Reference, Customer, Status, PaymentStatus, AmountTotal, Currency, SubscriptionPayload string }

// Stripe verifies the original signed payload and serializes the unchanged checkout request.
type Stripe interface {
	Checkout(checkout StripeCheckout) (string, error)
	Verify(payload []byte, signature string) (StripeEvent, error)
}
