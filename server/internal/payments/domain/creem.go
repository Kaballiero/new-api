package domain

import (
	"context"
)

type CreemProduct struct {
	ProductId, Name string
	Price           float64
	Currency        string
	Quota           int64
}

type CreemEvent struct {
	Type, Reference, Status, OrderType, Email, CustomerName, SubscriptionPayload string
	ID, OrderID, ProductName, Currency                                           string
	AmountPaid                                                                   int
}

// Creem sends checkout requests and verifies/decodes notifications without choosing their business destination.
type Creem interface {
	Checkout(ctx context.Context, reference string, product CreemProduct, email string, username string) (string, error)
	Verify(payload []byte, signature string) bool
	Parse(payload []byte) (CreemEvent, error)
}
