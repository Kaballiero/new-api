package model

import (
	"github.com/QuantumNous/new-api/constant"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type pricingOperation struct{ operation, main, basis string }

func configuredPricingOperation(channel *Channel, public, upstream string, endpoint constant.EndpointType) pricingOperation {
	v := pricingdomain.ConfiguredPricingOperation(pricingCapabilities(), pricingBuildChannel(channel), public, upstream, endpoint)
	return pricingOperation{v.Operation, v.Main, v.Basis}
}
func configuredOperationOffers(pricing Pricing, profile pricingOperation, mode, expression string, public ...bool) []CatalogOffer {
	return newPricingOperations().ConfiguredOperationOffers(pricing, pricingdomain.PricingOperation{Operation: profile.operation, Main: profile.main, Basis: profile.basis}, mode, expression, public...)
}
func projectPricingOperations(pricing *Pricing, abilities []AbilityWithChannel, channels map[int]*Channel, generation *jsplugin.RoutingGeneration, advanced map[int]*dto.AdvancedCustomConfig, keys map[int]string) {
	newPricingOperations().ProjectPricingOperations(pricing, pricingBuildAbilities(abilities), pricingBuildChannels(channels), pricingadapter.NewGeneration(generation), pricingadapter.AdvancedConfigurations(advanced), keys)
}
func pairedPricingBatchBase(ability AbilityWithChannel, upstream string, abilities []AbilityWithChannel, channels map[int]*Channel) (string, bool) {
	return newPricingOperations().PairedPricingBatchBase(pricingBuildAbilities([]AbilityWithChannel{ability})[0], upstream, pricingBuildAbilities(abilities), pricingBuildChannels(channels))
}
func ConsumerPricingCatalogue(pricing []Pricing, groups map[string]string) []Pricing {
	return pricingdomain.ConsumerPricingCatalogue(pricing, groups)
}
func imagePricingWithTokenInputs(offers []CatalogOffer) []CatalogOffer {
	return pricingdomain.ImagePricingWithTokenInputs(offers)
}

func configuredVeoUsageOffers(pricing Pricing, channel *Channel, upstream string, generation *jsplugin.RoutingGeneration) []CatalogOffer {
	return pricingdomain.ConfiguredVeoUsageOffers(pricingCapabilities(), pricing, pricingBuildChannel(channel), upstream, pricingadapter.NewGeneration(generation))
}
