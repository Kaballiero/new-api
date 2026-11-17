package pricingruntime

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	catalogstore "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"
	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/estimate_upstream_reserve"
	"github.com/QuantumNous/new-api/internal/pricing/read_catalogue"
)

var catalogueReader = sync.OnceValue(func() read_catalogue.Usecase {
	return read_catalogue.NewUsecase(pricingstate.PricingReadState{}, RebuildPricingCatalogueLocked)
})

func GetPricing() []pricingdomain.Pricing {
	return pricingstate.GetPricing(RebuildPricingCatalogueLocked)
}
func GetVendors() []pricingdomain.PricingVendor {
	return pricingstate.GetVendors(RebuildPricingCatalogueLocked)
}
func RefreshPricing() { pricingstate.RefreshPricing(RebuildPricingCatalogueLocked) }
func GetPricingCatalogueSnapshot() ([]pricingdomain.Pricing, []pricingdomain.PricingVendor, string, error) {
	return pricingstate.GetPricingCatalogueSnapshot(catalogueReader().Execute)
}
func GetPricingCatalogueSnapshotWithEndpoints() ([]pricingdomain.Pricing, []pricingdomain.PricingVendor, map[string]common.EndpointInfo, string, error) {
	return pricingstate.GetPricingCatalogueSnapshotWithEndpoints(catalogueReader().Execute)
}
func GetModelQuotaTypes(name string) []int {
	return pricingstate.GetModelQuotaTypes(name, func() { GetPricing() })
}
func NewCatalogPersistence() catalogrecord.Store {
	return catalogstore.NewCatalogPersistence(MutateModelPricingOptions)
}

// EstimateUpstreamReserve prices the OpenRouter reserve of a gateway request
// from the upstream catalog tariffs at most PRICING_CATALOG_MAX_AGE_HOURS
// (24 by default) old.
var EstimateUpstreamReserve = sync.OnceValue(func() estimate_upstream_reserve.Usecase {
	return estimate_upstream_reserve.NewUsecase(pricingadapter.Capabilities(), &UpstreamCatalogTariffs, time.Now, func() int {
		return common.GetEnvOrDefault("PRICING_CATALOG_MAX_AGE_HOURS", 24)
	})
})
