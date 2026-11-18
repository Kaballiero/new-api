package estimate_upstream_reserve

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/shopspring/decimal"
)

// searchCost is the cost of the web searches of a request on an endpoint
// tariff and whether a channel reserve makes it:
//   - requested native searches cost the web_search price of the endpoint
//     each, with no add-on reserve on top. A price missing from a fresh
//     catalog tariff is domain.ErrEndpointPriceMissing, checked against live
//     first; then the search reserve covers them, without it they are
//     domain.ErrHostedSearchUnpriced;
//   - a search on an engine without an endpoint price adds the search reserve;
//   - without requested searches the request costs no searches; the floor
//     reserve covers an endpoint that searches on its own;
//   - the searches of a decision model cost the web_search price of the
//     endpoint each, and nothing on an endpoint that publishes none. A missing
//     or invalid price is a *domain.EstimateUnavailableError: a fresh catalog
//     tariff is checked against live first, then only the local cover covers it.
//
// The channel reserves belong to the model of the request.
func (u Usecase) searchCost(in Input, endpoint domain.NativePricedEndpoint, tariff modelTariffs) (decimal.Decimal, bool, error) {
	if in.Decisions {
		raw, published := endpoint.Pricing["web_search"]
		if !published {
			return decimal.Zero, false, nil
		}
		rate, err := domain.ReadEndpointRate(u.deps, raw)
		if err != nil {
			return decimal.Zero, false, &domain.EstimateUnavailableError{Cause: err}
		}
		return rate.Mul(decimal.NewFromInt(int64(in.Searches))), false, nil
	}
	if in.Searches == 0 && !in.ManualSearch {
		return decimal.Zero, false, nil
	}
	cost := decimal.Zero
	reserves := 0
	if in.ManualSearch {
		reserves++
	}
	if in.Searches > 0 {
		rate, err := domain.ReadEndpointRate(u.deps, endpoint.Pricing["web_search"])
		switch {
		case err == nil:
			cost = rate.Mul(decimal.NewFromInt(int64(in.Searches)))
		case !errors.Is(err, domain.ErrEndpointPriceMissing) || tariff.checksLive():
			return decimal.Zero, false, err
		default:
			reserves++
		}
	}
	if reserves == 0 {
		return cost, false, nil
	}
	if in.SearchReserveUSD == nil {
		return decimal.Zero, false, domain.ErrHostedSearchUnpriced
	}
	if endpoint.ModelID != "" && endpoint.ModelID != in.Model {
		return decimal.Zero, false, errors.New("OpenRouter reserve endpoint model does not match final model ID")
	}
	return cost.Add(decimal.NewFromFloat(*in.SearchReserveUSD).Mul(decimal.NewFromInt(int64(reserves)))), true, nil
}
