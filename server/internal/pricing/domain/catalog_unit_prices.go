package domain

import (
	"slices"

	"github.com/shopspring/decimal"
)

// PriceUnit is the catalog unit a component prices.
type PriceUnit string

const (
	PriceUnitCharacter     PriceUnit = "character"
	PriceUnitUTF8Byte      PriceUnit = "utf8_byte"
	PriceUnitSecond        PriceUnit = "second"
	PriceUnitSearch        PriceUnit = "search"
	PriceUnitMillionTokens PriceUnit = "million_tokens"
)

// priceUnits are the units a unit price is read in; a component in another
// unit prices an unknown dimension.
var priceUnits = []PriceUnit{PriceUnitCharacter, PriceUnitUTF8Byte, PriceUnitSecond, PriceUnitSearch, PriceUnitMillionTokens}

// PriceBillable is the catalog billable a component prices.
type PriceBillable string

const (
	PriceBillableInputAudio   PriceBillable = "input_audio"
	PriceBillableInputText    PriceBillable = "input_text"
	PriceBillableOutputAudio  PriceBillable = "output_audio"
	PriceBillableOutputSpeech PriceBillable = "output_speech"
	PriceBillableSearch       PriceBillable = "search"
)

// PriceDimension is what a unit price bills: a billable in a unit. The zero
// dimension is an unknown one: a component without a billable or in a unit
// outside priceUnits.
type PriceDimension struct {
	Billable PriceBillable
	Unit     PriceUnit
}

// EndpointUnitPrice is the USD price of one unit of a dimension on an
// endpoint, for million_tokens the price of a million tokens; nil USD is
// unknown. PublishedUSD are the amounts every component of the dimension
// publishes, amountUSD / quantity and amountUSD itself: the catalog mirrors
// one of them, not necessarily USD, into the endpoint prompt or completion.
type EndpointUnitPrice struct {
	Dimension    PriceDimension
	USD          *decimal.Decimal
	PublishedUSD []decimal.Decimal
}

// CatalogUnitPrices are the unit prices of the model_page components of an
// endpoint offer, one per dimension in component order. A price is
// amountUSD / quantity, the maximum over the variants and utcDay conditions
// of its dimension; a null, negative or unreadable amount is unknown and
// keeps the dimension unknown.
func CatalogUnitPrices(deps Capabilities, components []SourceRate) []EndpointUnitPrice {
	var prices []EndpointUnitPrice
	positions := map[PriceDimension]int{}
	for _, component := range components {
		var provenance struct {
			Source string `json:"source"`
		}
		if deps.Codec.Unmarshal(component.Provenance, &provenance) != nil || provenance.Source != "model_page" {
			continue
		}
		var dimension PriceDimension
		unit := PriceUnit(component.Unit)
		if component.Billable != nil && slices.Contains(priceUnits, unit) {
			dimension = PriceDimension{Billable: PriceBillable(*component.Billable), Unit: unit}
		}
		var usd *decimal.Decimal
		var published []decimal.Decimal
		if component.AmountUSD != nil {
			amount, amountErr := decimal.NewFromString(*component.AmountUSD)
			quantity, quantityErr := decimal.NewFromString(component.Quantity)
			if amountErr == nil && quantityErr == nil && !amount.IsNegative() && quantity.IsPositive() {
				perUnit := amount.Div(quantity)
				usd = &perUnit
				published = []decimal.Decimal{perUnit, amount}
			}
		}
		position, priced := positions[dimension]
		if !priced {
			positions[dimension] = len(prices)
			prices = append(prices, EndpointUnitPrice{Dimension: dimension, USD: usd, PublishedUSD: published})
			continue
		}
		prices[position].PublishedUSD = append(prices[position].PublishedUSD, published...)
		switch {
		case prices[position].USD == nil || usd == nil:
			prices[position].USD = nil
		case usd.GreaterThan(*prices[position].USD):
			prices[position].USD = usd
		}
	}
	return prices
}
