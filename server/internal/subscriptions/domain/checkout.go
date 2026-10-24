package domain

import (
	"context"
)

type CheckoutUser struct {
	ID                              int
	Username, Email, StripeCustomer string
}
type CheckoutDiagnostics interface {
	Error(context.Context, string, ...any)
	Info(context.Context, string, ...any)
}
type OrderExpiry interface{ Expire(string, string) error }
