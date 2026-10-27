package controller

import (
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
)

// These fields describe the published endpoint schedule, not a request SKU.
type catalogPriceWindow = pricingdomain.CatalogPriceWindow

// Only expose opt-in prices in a minimum when the channel preserves that
// option. Explicit admin overrides above are applied after field removal.

// A native model-page meter can lack the endpoint's context schedule.
// Keep it in details; it cannot replace that schedule unconditionally.

func catalogRateWindow(price consumerProviderPrice) (catalogPriceWindow, bool) {
	return pricingdomain.CatalogRateWindow(pricingadapter.Capabilities(), price)
}

// A pathological snapshot must not multiply a list response without a
// bound. Unknown schedules are never rendered as an unconditional tier.

// The inner reduction only computes component minima; conditions
// have already been applied to this exact schedule cell.
