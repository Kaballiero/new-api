package estimate_upstream_reserve

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

// decisionOutputRates are the endpoint rates of an output a decisions request
// does not measure.
var decisionOutputRates = []string{"completion", "internal_reasoning"}

// assertDecisionOutputUnpriced requires the output rates of a decisions
// endpoint to be zero or absent: a decisions request measures no output, so a
// non-zero rate is an unmeasured quantity, a *domain.EstimateUnavailableError
// that only the local cover covers; a missing rate of a fresh catalog tariff
// is checked against live first.
func (u Usecase) assertDecisionOutputUnpriced(endpoint domain.NativePricedEndpoint) error {
	for _, key := range decisionOutputRates {
		raw, published := endpoint.Pricing[key]
		if !published {
			continue
		}
		rate, err := domain.ReadEndpointRate(u.deps, raw)
		if err != nil {
			return &domain.EstimateUnavailableError{Cause: err}
		}
		if !rate.IsZero() {
			return &domain.EstimateUnavailableError{Cause: fmt.Errorf("OpenRouter decisions output quantity is unmeasured: %s", key)}
		}
	}
	return nil
}
