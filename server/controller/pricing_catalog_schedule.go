package controller

import (
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
)

// These fields describe the published endpoint schedule, not a request SKU.
type catalogPriceWindow = pricingdomain.CatalogPriceWindow

type catalogScheduledPrice = pricingdomain.CatalogScheduledPrice

const catalogUnboundedContext int64 = 9007199254740991
const catalogMaxScheduleCells = 4096

func catalogServiceTier(variant []byte) string {
	return pricingdomain.CatalogServiceTier(variant)
}

func catalogServiceTierAllowed(route model.UpstreamCatalogRoute, offer catalogsync.Offer, requested string, only, order []string) bool {
	return pricingdomain.CatalogServiceTierAllowed(route, offer, requested, only, order)
}

// Only expose opt-in prices in a minimum when the channel preserves that
// option. Explicit admin overrides above are applied after field removal.

func catalogSchedulePrices(offer consumerProviderOffer) []consumerProviderPrice {
	return pricingdomain.
		CatalogSchedulePrices(offer)
}

// A native model-page meter can lack the endpoint's context schedule.
// Keep it in details; it cannot replace that schedule unconditionally.

func catalogRateWindow(price consumerProviderPrice) (catalogPriceWindow, bool) {
	return pricingdomain.
		CatalogRateWindow(price)
}

func catalogScheduleTiers(rates []catalogScheduledPrice) []publicPricingTier {
	return pricingdomain.
		CatalogScheduleTiers(rates)
}

// A pathological snapshot must not multiply a list response without a
// bound. Unknown schedules are never rendered as an unconditional tier.

// The inner reduction only computes component minima; conditions
// have already been applied to this exact schedule cell.

func catalogScheduledRates(offer consumerProviderOffer) []catalogScheduledPrice {
	return pricingdomain.
		CatalogScheduledRates(offer)
}
