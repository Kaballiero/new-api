package list_pancake_catalog

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

// Settings supplies persisted credentials only when both typed fields are blank.
type Settings interface {
	PancakeCredentials() domain.PancakeCredentials
}

type Input struct{ MerchantID, PrivateKey string }

type Usecase struct {
	diagnostics domain.Diagnostics
	factory     domain.PancakeFactory
	settings    Settings
}

func NewUsecase(factory domain.PancakeFactory, settings Settings, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, factory: factory, settings: settings}
}

func (u Usecase) Execute(ctx context.Context, in Input) (*domain.WaffoPancakeCatalog, error) {
	creds := domain.AdminCredentials(in.MerchantID, in.PrivateKey, u.settings.PancakeCredentials())
	if creds.MerchantID == "" || creds.PrivateKey == "" {
		return nil, domain.ErrCredentials
	}
	client, err := u.factory.Management(creds)
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo Pancake 拉取店铺与产品目录失败 error=%q", err)
		return nil, domain.ErrCatalog
	}
	catalog, err := client.Catalog(ctx)
	if err != nil {
		u.diagnostics.Error(ctx, "Waffo Pancake 拉取店铺与产品目录失败 error=%q", err)
		return nil, domain.ErrCatalog
	}
	for i := range catalog.Stores {
		active := catalog.Stores[i].OnetimeProducts[:0]
		for _, product := range catalog.Stores[i].OnetimeProducts {
			if strings.EqualFold(strings.TrimSpace(product.Status), "active") {
				active = append(active, product)
			}
		}
		catalog.Stores[i].OnetimeProducts = active
	}
	return catalog, nil
}
