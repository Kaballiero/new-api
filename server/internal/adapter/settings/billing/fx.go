package billing

import (
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

func ReadFX() (domain.FXPublication, error) {
	s, err := model.CurrentModelCostFX(service.ModelCostFXSource)
	if err != nil {
		return domain.FXPublication{}, err
	}
	return domain.FXPublication{Source: s.Source, Version: s.Version, EffectiveAt: s.EffectiveAt, FetchedAt: s.FetchedAt, Rates: s.Rates}, nil
}
