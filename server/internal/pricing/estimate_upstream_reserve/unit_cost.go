package estimate_upstream_reserve

import (
	"errors"
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/shopspring/decimal"
)

// tokensPerMillion converts a million_tokens price to the price of a token.
var tokensPerMillion = decimal.NewFromInt(1_000_000)

// outputAudioTokens is the dimension of the output audio tokens, measured by
// the output cap of the endpoint rather than by the request.
var outputAudioTokens = domain.PriceDimension{Billable: domain.PriceBillableOutputAudio, Unit: domain.PriceUnitMillionTokens}

// unitPriceMirrorKeys are the endpoint pricing rates the catalog mirrors a
// unit price into.
var unitPriceMirrorKeys = []string{"prompt", "completion"}

// unitCost prices the measured units of an operation from the unit prices of
// an endpoint; the output audio tokens are outputTokens, the output cap of
// the endpoint (owner rule: model output cap × price). A zero price bills
// nothing. An endpoint without unit prices (a
// live one), an unknown price or a non-zero price of an unmeasured dimension
// is an unavailable estimate; live is not checked again, as it publishes no
// unit prices. So is a non-zero endpoint pricing rate the unit prices do not
// cover: the catalog mirrors a unit price into prompt or completion, per
// token for million_tokens and after the endpoint discount (heygen); any
// other non-zero rate, or an unreadable one, would bill beyond the units.
func (u Usecase) unitCost(endpoint domain.NativePricedEndpoint, units map[domain.PriceDimension]int64, outputTokens int) (decimal.Decimal, error) {
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
	discount := decimal.Zero
	if raw, ok := endpoint.Pricing["discount"]; ok {
		var err error
		if discount, err = domain.ReadEndpointRate(u.deps, raw); err != nil {
			return decimal.Zero, &domain.EstimateUnavailableError{Cause: errors.New("OpenRouter endpoint discount is unreadable")}
		}
	}
	for key, raw := range endpoint.Pricing {
		if key == "discount" {
			continue
		}
		rate, err := domain.ReadEndpointRate(u.deps, raw)
		if err != nil {
			return decimal.Zero, &domain.EstimateUnavailableError{Cause: fmt.Errorf("OpenRouter endpoint price %s is unreadable", key)}
		}
		if rate.IsZero() {
			continue
		}
		mirrorsUnitPrice := slices.ContainsFunc(endpoint.UnitPrices, func(price domain.EndpointUnitPrice) bool {
			perUnit := *price.USD
			if price.Dimension.Unit == domain.PriceUnitMillionTokens {
				perUnit = perUnit.Div(tokensPerMillion)
			}
			return rate.Equal(perUnit) || rate.Equal(perUnit.Mul(decimal.NewFromInt(1).Sub(discount)))
		})
		if !slices.Contains(unitPriceMirrorKeys, key) || !mirrorsUnitPrice {
			return decimal.Zero, &domain.EstimateUnavailableError{Cause: fmt.Errorf("OpenRouter endpoint price %s is not covered by its unit prices", key)}
		}
	}
	return cost, nil
}
