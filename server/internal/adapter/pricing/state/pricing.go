package pricingstate

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/constant"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

var (
	pricingMap         []pricingdomain.Pricing
	vendorsList        []pricingdomain.PricingVendor
	lastGetPricingTime time.Time
	updatePricingLock  sync.Mutex
	pricingRevision    atomic.Uint64

	// Cache maps model names to enabled groups and billing types.
	modelEnableGroups     = make(map[string][]string)
	modelQuotaTypeMap     = make(map[string]int)
	modelEnableGroupsLock = sync.RWMutex{}
)

var (
	modelSupportEndpointTypes = make(map[string][]constant.EndpointType)
	modelSupportEndpointsLock = sync.RWMutex{}
)

func refreshPricingCatalogueLocked(rebuild func()) {
	if (pricingBuiltGeneration != pricingInputsGeneration.Load() || lastGetPricingTime.IsZero() || pricingBuiltPluginGeneration != jsplugin.DefaultRegistry.Generation().Number) && pricingBuildDue() {
		modelSupportEndpointsLock.Lock()
		defer modelSupportEndpointsLock.Unlock()
		rebuild()
	}
}

func GetPricing(rebuild func()) []pricingdomain.Pricing {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()
	refreshPricingCatalogueLocked(rebuild)
	return pricingMap
}

func InvalidatePricingCache() {
	pricingPublicationLock.Lock()
	defer pricingPublicationLock.Unlock()
	pricingInputsGeneration.Add(1)
}

func GetVendors(rebuild func()) []pricingdomain.PricingVendor {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()
	refreshPricingCatalogueLocked(rebuild)
	return vendorsList
}

func GetModelSupportEndpointTypes(model string) []constant.EndpointType {
	if model == "" {
		return make([]constant.EndpointType, 0)
	}
	modelSupportEndpointsLock.RLock()
	defer modelSupportEndpointsLock.RUnlock()
	if endpoints, ok := modelSupportEndpointTypes[model]; ok {
		return endpoints
	}
	return make([]constant.EndpointType, 0)
}

// GetSupportedEndpointMap returns the published endpoint paths.
func pricingCapabilities() pricingdomain.Capabilities { return pricingadapter.Capabilities() }

func pricingCatalogDocument(value *catalogsync.Document) *pricingdomain.SourceDocument {
	if value == nil {
		return nil
	}
	result := pricingadapter.ToSourceDocument(*value)
	return &result
}
