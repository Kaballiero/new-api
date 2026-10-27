package pricingstate

import (
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	legacy "github.com/QuantumNous/new-api/model"
)

func ResolvePricingUpstreamModel(channel *legacy.Channel, modelName string) (string, bool) {
	return pricingdomain.ResolvePricingUpstreamModel(pricingCapabilities(), PricingBuildChannel(channel), modelName)
}
