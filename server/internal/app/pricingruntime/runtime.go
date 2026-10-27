package pricingruntime

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	catalogstore "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"

	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"

	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
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
