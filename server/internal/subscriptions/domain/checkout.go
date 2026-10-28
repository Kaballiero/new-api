package domain

type CheckoutUser struct {
	ID                              int
	Username, Email, StripeCustomer string
}
