package estimate_upstream_reserve

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/shopspring/decimal"
)

// tokensPerMillion converts a million_tokens price to the price of a token.
var tokensPerMillion = decimal.NewFromInt(1_000_000)

// outputAudioTokens is the dimension of the output audio tokens, measured by
// the output cap of the endpoint rather than by the request.
var outputAudioTokens = domain.PriceDimension{Billable: domain.PriceBillableOutputAudio, Unit: domain.PriceUnitMillionTokens}

// unitCost prices the measured units of an operation from the unit prices of
// an endpoint; the output audio tokens are outputTokens, the output cap of
// the endpoint (owner rule: model output cap × price). A zero price bills
// nothing. An endpoint without unit prices (a
// live one), an unknown price or a non-zero price of an unmeasured dimension
// is an unavailable estimate; live is not checked again, as it publishes no
// unit prices.
func unitCost(endpoint domain.NativePricedEndpoint, units map[domain.PriceDimension]int64, outputTokens int) (decimal.Decimal, error) {
	if len(endpoint.UnitPrices) == 0 {
		return decimal.Zero, &domain.EstimateUnavailableError{Cause: errors.New("OpenRouter endpoint unit prices are unknown")}
	}
	cost := decimal.Zero
	for _, price := range endpoint.UnitPrices {
		if price.USD == nil {
			return decimal.Zero, &domain.EstimateUnavailableError{Cause: errors.New("OpenRouter endpoint unit price is unknown")}
		}
		if price.USD.IsZero() {
			continue
		}
		quantity, measured := units[price.Dimension]
		if price.Dimension == outputAudioTokens {
			quantity, measured = int64(outputTokens), true
		}
		if !measured {
			return decimal.Zero, &domain.EstimateUnavailableError{Cause: fmt.Errorf("OpenRouter unit quantity is unmeasured: %s/%s", price.Dimension.Billable, price.Dimension.Unit)}
		}
		amount := price.USD.Mul(decimal.NewFromInt(quantity))
		if price.Dimension.Unit == domain.PriceUnitMillionTokens {
			amount = amount.Div(tokensPerMillion)
		}
		cost = cost.Add(amount)
	}
	return cost, nil
}
