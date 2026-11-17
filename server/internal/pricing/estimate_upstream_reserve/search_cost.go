package estimate_upstream_reserve

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/shopspring/decimal"
)

// searchCost is the cost of the web searches of a request on an endpoint
// tariff and whether a channel reserve makes it:
//   - requested searches cost the web_search price of the endpoint each, with
//     no add-on reserve on top. A price missing from a fresh catalog tariff is
//     domain.ErrEndpointPriceMissing, checked against live first; then the
//     search reserve covers them, without it they are
//     domain.ErrHostedSearchUnpriced;
//   - the search reserve covers searches no endpoint price measures;
//   - without requested searches, an endpoint with a positive web_search price
//     may search on its own and the add-on reserve covers it.
//
// The channel reserves belong to the model of the request.
func (u Usecase) searchCost(in Input, endpoint domain.NativePricedEndpoint, tariff modelTariffs) (decimal.Decimal, bool, error) {
	if in.Searches > 0 {
		rate, err := domain.ReadEndpointRate(u.deps, endpoint.Pricing["web_search"])
		if err == nil {
			return rate.Mul(decimal.NewFromInt(int64(in.Searches))), false, nil
		}
		if !errors.Is(err, domain.ErrEndpointPriceMissing) || tariff.checksLive() {
			return decimal.Zero, false, err
		}
		if in.SearchReserveUSD == nil {
			return decimal.Zero, false, domain.ErrHostedSearchUnpriced
		}
	}
	if in.SearchReserveUSD != nil {
		if endpoint.ModelID != "" && endpoint.ModelID != in.Model {
			return decimal.Zero, false, errors.New("OpenRouter reserve endpoint model does not match final model ID")
		}
		return decimal.NewFromFloat(*in.SearchReserveUSD), true, nil
	}
	raw, published := endpoint.Pricing["web_search"]
	if !published || in.AddOnReserveUSD == nil {
		return decimal.Zero, false, nil
	}
	rate, err := domain.ReadEndpointRate(u.deps, raw)
	if err != nil || rate.IsZero() {
		return decimal.Zero, false, err
	}
	return decimal.NewFromFloat(*in.AddOnReserveUSD), true, nil
}
