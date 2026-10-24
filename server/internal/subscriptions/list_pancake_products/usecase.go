package list_pancake_products

import (
	"context"
	payments "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"strings"
)

type Result struct {
	StoreID string
	Catalog *payments.WaffoPancakeCatalog
}
type Usecase struct {
	settings    domain.CatalogSettings
	factory     payments.PancakeFactory
	diagnostics payments.Diagnostics
}

func NewUsecase(settings domain.CatalogSettings, factory payments.PancakeFactory, diagnostics payments.Diagnostics) Usecase {
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
		catalog, err = payments.LoadActivePancakeCatalog(ctx, u.factory, creds, u.diagnostics)
	}
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo Pancake 拉取订阅产品列表失败 store_id=%q error=%q", storeID, err.Error())
		return Result{}, domain.ErrProductCatalog
	}
	return Result{storeID, catalog}, nil
}
