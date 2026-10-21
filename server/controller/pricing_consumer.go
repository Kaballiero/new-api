package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type consumerPricingResponse = pricingdomain.ConsumerPricingResponse

type consumerPricingModel = pricingdomain.ConsumerPricingModel

type consumerGroupPricing = pricingdomain.ConsumerGroupPricing

type consumerPricingOperation = pricingdomain.ConsumerPricingOperation

type consumerUnitPrice = pricingdomain.ConsumerUnitPrice

func buildConsumerPricingResponse(userGroup string, basis service.BillingFXBasis, pricing []model.Pricing) consumerPricingResponse {
	return app.NewPricingProjection().BuildConsumerPricingResponse(userGroup, pricingdomain.BillingFXBasis(basis), pricing)
}

func buildConsumerPricingModel(usableGroups map[string]string, groupRatios map[string]float64, fxFactor float64, item model.Pricing, details bool, users ...pricingUserContext) consumerPricingModel {
	return app.NewPricingProjection().BuildConsumerPricingModel(usableGroups,
		groupRatios, fxFactor, item, details, users...)
}

func confirmedConsumerPrices(prices []consumerUnitPrice, tiers []publicPricingTier) bool {
	return pricingdomain.ConfirmedConsumerPrices(prices, tiers)
}

func consumerOperations(item model.Pricing, group string, ratio float64, validRatio bool) []consumerPricingOperation {
	return pricingdomain.
		ConsumerOperations(item, group, ratio,
			validRatio)
}
