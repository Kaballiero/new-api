package capture_fx_basis

import (
	"math"

	"github.com/QuantumNous/new-api/internal/billing/domain"
)

type Usecase struct{ settings domain.FXSettings }

func NewUsecase(settings domain.FXSettings) Usecase { return Usecase{settings: settings} }
func (u Usecase) Execute(captured domain.FXBasis) (domain.FXBasis, error) {
	if err := domain.ValidateFXAccounting(u.settings); err != nil {
		return domain.FXBasis{}, err
	}
	if captured.Factor != 0 {
		if captured.USDRate <= 0 || math.IsNaN(captured.USDRate) || math.IsInf(captured.USDRate, 0) || captured.USDRate/domain.FXRateDenomination != captured.Factor {
			return domain.FXBasis{}, &domain.FXError{Message: "captured model cost FX basis is invalid"}
		}
		return captured, nil
	}
	snapshot, err := u.settings.Read()
	if err != nil {
		return domain.FXBasis{}, &domain.FXError{Message: "model cost FX is unavailable"}
	}
	rate, err := domain.PublishedUSD(snapshot)
	if err != nil {
		return domain.FXBasis{}, err
	}
	factor := rate / domain.FXRateDenomination
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return domain.FXBasis{}, &domain.FXError{Message: "model cost FX basis is invalid"}
	}
	return domain.FXBasis{USDRate: rate, Factor: factor, Source: snapshot.Source, PublicationVersion: snapshot.Version, EffectiveAt: snapshot.EffectiveAt, FetchedAt: snapshot.FetchedAt}, nil
}
