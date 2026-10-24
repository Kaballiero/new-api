package list_pancake_products

import (
	"context"
	"strings"

	payments "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

type CatalogSettings interface {
	PancakeCredentials() payments.PancakeCredentials
	PancakeStore() string
}

type Diagnostics interface {
	Error(context.Context, string, ...any)
}

type Result struct {
	StoreID string
	Catalog *payments.WaffoPancakeCatalog
}
type Usecase struct {
	settings    CatalogSettings
	factory     payments.PancakeFactory
	diagnostics Diagnostics
}

func NewUsecase(settings CatalogSettings, factory payments.PancakeFactory, diagnostics Diagnostics) Usecase {
	return Usecase{settings, factory, diagnostics}
}
func (u Usecase) Execute(ctx context.Context) (Result, error) {
	creds := u.settings.PancakeCredentials()
	storeID := strings.TrimSpace(u.settings.PancakeStore())
	if creds.MerchantID == "" || creds.PrivateKey == "" || storeID == "" {
		return Result{}, domain.ErrPancakeBinding
	}
	// Catalog admission previously trimmed provided credentials through the payments reader.
	creds = payments.AdminCredentials(creds.MerchantID, creds.PrivateKey, u.settings.PancakeCredentials())
	var catalog *payments.WaffoPancakeCatalog
	var err error
	if creds.MerchantID == "" || creds.PrivateKey == "" {
		err = payments.ErrCredentials
	} else {
		client, buildErr := u.factory.Management(creds)
		err = buildErr
		if err == nil {
			catalog, err = client.Catalog(ctx)
		}
		if err != nil {
			u.diagnostics.Error(ctx, "Waffo Pancake store and product catalog query failed error=%q", err)
			err = payments.ErrCatalog
		} else {
			payments.FilterActivePancakeProducts(catalog)
		}
	}
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo Pancake 拉取订阅产品列表失败 store_id=%q error=%q", storeID, err.Error())
		return Result{}, domain.ErrProductCatalog
	}
	return Result{storeID, catalog}, nil
}
