package controller

import (
	"github.com/QuantumNous/new-api/internal/app"

	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
)

type consumerPricingResponse = pricingdomain.ConsumerPricingResponse

type consumerPricingModel = pricingdomain.ConsumerPricingModel

type consumerGroupPricing = pricingdomain.ConsumerGroupPricing

type consumerPricingOperation = pricingdomain.ConsumerPricingOperation

type consumerUnitPrice = pricingdomain.ConsumerUnitPrice

func buildConsumerPricingResponse(userGroup string, basis pricingdomain.BillingFXBasis, pricing []pricingdomain.Pricing) consumerPricingResponse {
	return app.NewPricingProjection().BuildConsumerPricingResponse(userGroup, basis, pricing)
}

func buildConsumerPricingModel(usableGroups map[string]string, groupRatios map[string]float64, fxFactor float64, item pricingdomain.Pricing, details bool, users ...pricingUserContext) consumerPricingModel {
	return app.NewPricingProjection().BuildConsumerPricingModel(usableGroups,
		groupRatios, fxFactor, item, details, users...)
}

func confirmedConsumerPrices(prices []consumerUnitPrice, tiers []publicPricingTier) bool {
	return pricingdomain.ConfirmedConsumerPrices(prices, tiers)
}

func consumerOperations(item pricingdomain.Pricing, group string, ratio float64, validRatio bool) []consumerPricingOperation {
	return pricingdomain.
		ConsumerOperations(item, group, ratio,
			validRatio)
}
