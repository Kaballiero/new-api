package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

type WaffoPancakePriceSnapshot struct {
	Amount      string
	TaxCategory string
}

// WaffoPancakeCreateSessionParams is the input to CreateWaffoPancakeCheckoutSession.
// BuyerIdentity must be stable per user (see WaffoPancakeBuyerIdentityFromUserID).
// OrderMerchantExternalID = our trade_no; Pancake echoes it back in webhooks.
type WaffoPancakeCreateSessionParams struct {
	ProductID               string
	BuyerIdentity           string
	PriceSnapshot           *WaffoPancakePriceSnapshot
	BuyerEmail              string
	ExpiresInSeconds        *int
	OrderMerchantExternalID string
}

// WaffoPancakeCheckoutSession is the response of CreateWaffoPancakeCheckoutSession.
// CheckoutURL already carries the `#token=...` fragment; Token / TokenExpiresAt
// are exposed separately for self-service flows driven from new-api's own UI.
type WaffoPancakeCheckoutSession struct {
	SessionID      string
	CheckoutURL    string
	ExpiresAt      string
	OrderID        string
	Token          string
	TokenExpiresAt string
}

// WaffoPancakeWebhookEvent mirrors the SDK's WebhookEvent shape using plain
// strings so controllers don't have to import the SDK package.
type WaffoPancakeWebhookEvent struct {
	ID        string
	Timestamp string
	EventType string
	EventID   string
	StoreID   string
	Mode      string
	Data      WaffoPancakeWebhookData
}

type WaffoPancakeWebhookData struct {
	// OrderID = Pancake ORD_* (logs); OrderMerchantExternalID = our trade_no (lookup).
	OrderID                       string
	OrderMerchantExternalID       string
	BuyerEmail                    string
	Currency                      string
	Amount                        string
	TaxAmount                     string
	ProductName                   string
	MerchantProvidedBuyerIdentity string
}

// NormalizedEventType returns the event type or empty string for a nil event.
func (e *WaffoPancakeWebhookEvent) NormalizedEventType() string {
	if e == nil {
		return ""
	}
	return e.EventType
}

type WaffoPancakeCatalogProduct struct {
	ID     string
	Name   string
	Status string
}

// WaffoPancakeCatalogStore nests its OnetimeProducts so the UI can render a
// dependent store→product select without a second round-trip.
type WaffoPancakeCatalogStore struct {
	ID              string
	Name            string
	Status          string
	ProdEnabled     bool
	OnetimeProducts []WaffoPancakeCatalogProduct
}

type WaffoPancakeCatalog struct {
	Stores []WaffoPancakeCatalogStore
}

type PancakeCredentials struct{ MerchantID, PrivateKey string }

type PancakeProduct struct{ StoreID, Name, Amount, ReturnURL string }

// PancakeCheckout sends the authenticated two-request checkout and verifies the SDK webhook protocol.
type PancakeCheckout interface {
	Checkout(ctx context.Context, params *WaffoPancakeCreateSessionParams) (*WaffoPancakeCheckoutSession, error)
	Verify(payload string, signature string) (*WaffoPancakeWebhookEvent, error)
}

// PancakeManagement executes individual provider operations with one credential-bound SDK client.
type PancakeManagement interface {
	Catalog(ctx context.Context) (*WaffoPancakeCatalog, error)
	CreateStore(ctx context.Context, name string) (string, error)
	CreateProduct(ctx context.Context, product PancakeProduct) (string, error)
	Publish(ctx context.Context, productID string) error
}

// PancakeFactory builds a client from explicit credentials; it never resolves credentials or saves options.
type PancakeFactory interface {
	Management(credentials PancakeCredentials) (PancakeManagement, error)
}

const PancakeStoreName = "new-api-store"

const PancakeProductName = "new-api-charge-product"

func PancakeBuyerIdentity(userID int) string { return fmt.Sprintf("new-api-user-%d", userID) }

func PancakeBuyerEmail(email string) string {
	if strings.TrimSpace(email) == "" {
		return ""
	}
	return email
}

func AdminCredentials(merchant, private string, persisted PancakeCredentials) PancakeCredentials {
	merchant, private = strings.TrimSpace(merchant), strings.TrimSpace(private)
	if merchant == "" && private == "" {
		return persisted
	}
	return PancakeCredentials{MerchantID: merchant, PrivateKey: private}
}

// CreatePancakeCheckout validates checkout ownership before invoking the provider adapter.
func CreatePancakeCheckout(ctx context.Context, client PancakeCheckout, params *WaffoPancakeCreateSessionParams) (*WaffoPancakeCheckoutSession, error) {
	if params == nil {
		return nil, fmt.Errorf("missing checkout params")
	}
	if strings.TrimSpace(params.BuyerIdentity) == "" {
		return nil, fmt.Errorf("missing buyer identity")
	}
	if strings.TrimSpace(params.OrderMerchantExternalID) == "" {
		return nil, fmt.Errorf("missing order merchant external id")
	}
	return client.Checkout(ctx, params)
}

func PancakeAmount(money float64) string { return decimal.NewFromFloat(money).StringFixed(2) }
