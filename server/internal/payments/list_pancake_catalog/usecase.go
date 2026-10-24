package list_pancake_catalog

import (
	"context"

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
	return domain.LoadActivePancakeCatalog(ctx, u.factory, creds, u.diagnostics)
}
