package pancake

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/setting"

	pancake "github.com/waffo-com/waffo-pancake-sdk-go"
)

type Client struct{}

type Factory struct{}

type Management struct{ client *pancake.Client }

func newWaffoPancakeClient() (*pancake.Client, error) {
	return pancake.New(pancake.Config{
		MerchantID: setting.WaffoPancakeMerchantID,
		PrivateKey: setting.WaffoPancakePrivateKey,
	})
}

func newWaffoPancakeClientFromCreds(merchantID, privateKey string) (*pancake.Client, error) {
	if strings.TrimSpace(merchantID) == "" || strings.TrimSpace(privateKey) == "" {
		return nil, fmt.Errorf("merchant id and private key are required")
	}
	return pancake.New(pancake.Config{
		MerchantID: merchantID,
		PrivateKey: privateKey,
	})
}

func (Client) Checkout(ctx context.Context, params *domain.WaffoPancakeCreateSessionParams) (*domain.WaffoPancakeCheckoutSession, error) {
	client, err := newWaffoPancakeClient()
	if err != nil {
		return nil, fmt.Errorf("build Waffo Pancake client: %w", err)
	}

	sdkParams := pancake.AuthenticatedCheckoutParams{
		CreateCheckoutSessionParams: pancake.CreateCheckoutSessionParams{
			ProductID:               params.ProductID,
			Currency:                "USD",
			BuyerEmail:              optionalString(params.BuyerEmail),
			ExpiresInSeconds:        params.ExpiresInSeconds,
			OrderMerchantExternalID: optionalString(params.OrderMerchantExternalID),
		},
		BuyerIdentity: params.BuyerIdentity,
	}
	if params.PriceSnapshot != nil {
		sdkParams.PriceSnapshot = &pancake.PriceInfo{
			Amount:      params.PriceSnapshot.Amount,
			TaxCategory: pancake.TaxCategory(params.PriceSnapshot.TaxCategory),
		}
	}

	session, err := client.Checkout.Authenticated.Create(ctx, sdkParams)
	if err != nil {
		return nil, err
	}
	if session == nil || strings.TrimSpace(session.CheckoutURL) == "" || strings.TrimSpace(session.SessionID) == "" {
		return nil, fmt.Errorf("Waffo Pancake returned empty checkout session")
	}
	return &domain.WaffoPancakeCheckoutSession{
		SessionID:      session.SessionID,
		CheckoutURL:    session.CheckoutURL,
		ExpiresAt:      session.ExpiresAt,
		Token:          session.Token,
		TokenExpiresAt: session.TokenExpiresAt,
	}, nil
}

func optionalString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	v := s
	return &v
}

func (Client) Verify(payload string, signatureHeader string) (*domain.WaffoPancakeWebhookEvent, error) {
	evt, err := pancake.VerifyWebhookTyped[pancake.WebhookEventData](payload, signatureHeader, nil)
	if err != nil {
		return nil, err
	}
	identity := ""
	if evt.Data.MerchantProvidedBuyerIdentity != nil {
		identity = *evt.Data.MerchantProvidedBuyerIdentity
	}
	externalID := ""
	if evt.Data.OrderMerchantExternalID != nil {
		externalID = *evt.Data.OrderMerchantExternalID
	}
	return &domain.WaffoPancakeWebhookEvent{
		ID:        evt.ID,
		Timestamp: evt.Timestamp,
		EventType: evt.EventType,
		EventID:   evt.EventID,
		StoreID:   evt.StoreID,
		Mode:      string(evt.Mode),
		Data: domain.WaffoPancakeWebhookData{
			OrderID:                       evt.Data.OrderID,
			OrderMerchantExternalID:       externalID,
			BuyerEmail:                    evt.Data.BuyerEmail,
			Currency:                      evt.Data.Currency,
			Amount:                        evt.Data.Amount,
			TaxAmount:                     evt.Data.TaxAmount,
			ProductName:                   evt.Data.ProductName,
			MerchantProvidedBuyerIdentity: identity,
		},
	}, nil
}

type WaffoPancakeCatalogProduct struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// WaffoPancakeCatalogStore nests its OnetimeProducts so the UI can render a
// dependent store→product select without a second round-trip.
type WaffoPancakeCatalogStore struct {
	ID              string                       `json:"id"`
	Name            string                       `json:"name"`
	Status          string                       `json:"status"`
	ProdEnabled     bool                         `json:"prodEnabled"`
	OnetimeProducts []WaffoPancakeCatalogProduct `json:"onetimeProducts"`
}

type WaffoPancakeCatalog struct {
	Stores []WaffoPancakeCatalogStore `json:"stores"`
}

func (m Management) Catalog(ctx context.Context) (*domain.WaffoPancakeCatalog, error) {
	client := m.client

	type queryShape struct {
		Stores []WaffoPancakeCatalogStore `json:"stores"`
	}
	// `limit: 100` because the API returns a single store when limit is
	// omitted, even for multi-store merchants. Bump to paginated fetches
	// (via `offset`) if real catalogs ever cross the cap.
	resp, err := pancake.GraphQLQuery[queryShape](ctx, client, pancake.GraphQLParams{
		Query: `query {
			stores(limit: 100) {
				id
				name
				status
				prodEnabled
				onetimeProducts {
					id
					name
					status
				}
			}
		}`,
	})
	if err != nil {
		return nil, fmt.Errorf("query Waffo Pancake catalog: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("waffo pancake catalog query returned %d errors: %s",
			len(resp.Errors), resp.Errors[0].Message)
	}
	var stores []domain.WaffoPancakeCatalogStore
	if resp.Data.Stores != nil {
		stores = make([]domain.WaffoPancakeCatalogStore, len(resp.Data.Stores))
	}
	for i, store := range resp.Data.Stores {
		var products []domain.WaffoPancakeCatalogProduct
		if store.OnetimeProducts != nil {
			products = make([]domain.WaffoPancakeCatalogProduct, len(store.OnetimeProducts))
		}
		for j, p := range store.OnetimeProducts {
			products[j] = domain.WaffoPancakeCatalogProduct{ID: p.ID, Name: p.Name, Status: p.Status}
		}
		stores[i] = domain.WaffoPancakeCatalogStore{ID: store.ID, Name: store.Name, Status: store.Status, ProdEnabled: store.ProdEnabled, OnetimeProducts: products}
	}
	return &domain.WaffoPancakeCatalog{Stores: stores}, nil
}

func (Factory) Management(in domain.PancakeCredentials) (domain.PancakeManagement, error) {
	client, err := newWaffoPancakeClientFromCreds(in.MerchantID, in.PrivateKey)
	if err != nil {
		return nil, err
	}
	return Management{client: client}, nil
}

func (m Management) CreateStore(ctx context.Context, name string) (string, error) {
	result, err := m.client.Stores.Create(ctx, pancake.CreateStoreParams{Name: name})
	if err != nil {
		return "", err
	}
	return result.Store.ID, nil
}

func (m Management) CreateProduct(ctx context.Context, in domain.PancakeProduct) (string, error) {
	result, err := m.client.OnetimeProducts.Create(
		ctx,
		pancake.CreateOnetimeProductParams{
			StoreID:    in.StoreID,
			Name:       in.Name,
			Prices:     pancake.Prices{"USD": {Amount: in.Amount, TaxCategory: pancake.TaxCategory("saas")}},
			SuccessURL: optionalString(strings.TrimSpace(in.ReturnURL)),
		},
	)
	if err != nil {
		return "", err
	}
	return result.Product.ID, nil
}

func (m Management) Publish(ctx context.Context, productID string) error {
	_, err := m.client.OnetimeProducts.Publish(ctx, pancake.PublishOnetimeProductParams{ID: productID})
	return err
}
