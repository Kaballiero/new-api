package create_pancake_pair

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

// Settings supplies persisted credentials without normalizing the stored values.
type Settings interface {
	PancakeCredentials() domain.PancakeCredentials
}

type Input struct{ MerchantID, PrivateKey, ReturnURL string }

type Result struct {
	StoreID, StoreName, ProductID, ProductName string
	OrphanStore                                bool
}

type Usecase struct {
	diagnostics domain.Diagnostics
	factory     domain.PancakeFactory
	settings    Settings
}

func NewUsecase(factory domain.PancakeFactory, settings Settings, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, factory: factory, settings: settings}
}

func (u Usecase) Execute(ctx context.Context, in Input) (*Result, error) {
	creds := domain.AdminCredentials(in.MerchantID, in.PrivateKey, u.settings.PancakeCredentials())
	if creds.MerchantID == "" || creds.PrivateKey == "" {
		return nil, domain.ErrCredentials
	}
	client, err := u.factory.Management(creds)
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo Pancake store and product creation failed orphan_store=%t store_id=%q error=%q", false, "", err)
		return nil, err
	}
	store, err := client.CreateStore(ctx, domain.PancakeStoreName)
	if err != nil {
		err = fmt.Errorf("create Waffo Pancake store: %w", err)
		u.diagnostics.Error(ctx, "Waffo Pancake store and product creation failed orphan_store=%t store_id=%q error=%q", false, "", err)
		return nil, err
	}
	result := &Result{StoreID: store, StoreName: domain.PancakeStoreName, OrphanStore: true}
	product, err := u.product(ctx, creds, store, in.ReturnURL)
	if err != nil {
		err = fmt.Errorf("store created at %s but product creation failed: %w", store, err)
		u.diagnostics.Error(ctx, "Waffo Pancake store and product creation failed orphan_store=%t store_id=%q error=%q", true, store, err)
		return result, err
	}
	result.ProductID = product
	result.ProductName = domain.PancakeProductName
	result.OrphanStore = false
	return result, nil
}

// product creates and publishes the wallet product using one SDK client, after the store was created.
func (u Usecase) product(ctx context.Context, creds domain.PancakeCredentials, store, back string) (string, error) {
	store = strings.TrimSpace(store)
	if store == "" {
		return "", fmt.Errorf("store id is required to create a product")
	}
	client, err := u.factory.Management(creds)
	if err != nil {
		return "", err
	}
	product, err := client.CreateProduct(ctx, domain.PancakeProduct{StoreID: store, Name: domain.PancakeProductName, Amount: "1.00", ReturnURL: back})
	if err != nil {
		return "", fmt.Errorf("create Waffo Pancake product: %w", err)
	}
	if err := client.Publish(ctx, product); err != nil {
		return "", fmt.Errorf("publish Waffo Pancake product: %w", err)
	}
	return product, nil
}
