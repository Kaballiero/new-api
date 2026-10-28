package billing

import (
	fxstate "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"
)

func ReadFX() (domain.FXPublication, error) {
	s, err := fxstate.CurrentModelCostFX(pricingdomainfx.ModelCostFXSource)
	if err != nil {
		return domain.FXPublication{}, err
	}
	return domain.FXPublication{Source: s.Source, Version: s.Version, EffectiveAt: s.EffectiveAt, FetchedAt: s.FetchedAt, Rates: s.Rates}, nil
}
