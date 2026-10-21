package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
)

type upstreamRate = pricingdomain.UpstreamRate

type consumerProviderPrice = pricingdomain.ConsumerProviderPrice

type consumerProviderOffer = pricingdomain.ConsumerProviderOffer

type pricingUserContext = pricingdomain.PricingUserContext

func catalogComponentName(rate catalogsync.Component) string {
	return pricingdomain.CatalogComponentName(rate)
}

// Legacy meters can supply semantics only when their dimensions agree.

// Headers, payload parameters, token selection and retries are absent from a
// catalog request. Do not evaluate their conditional branches as known false.

// Moving a client's legacy provider object into its canonical field
// does not introduce a server-side provider constraint.

func upstreamProviderOffers(item model.Pricing, group string, ratio float64, validRatio bool, user pricingUserContext) []consumerProviderOffer {
	return app.NewPricingProjection().UpstreamProviderOffers(item, group, ratio, validRatio,
		user)
}

func catalogMinima(offers []consumerProviderOffer) []consumerPricingOperation {
	return pricingdomain.
		CatalogMinima(offers)
}

func mergeCatalogMinima(existing, upstream []consumerPricingOperation) []consumerPricingOperation {
	return pricingdomain.MergeCatalogMinima(existing, upstream)
}
