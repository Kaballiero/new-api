package pricingstate

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/read_catalogue"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

type publishedPricingCatalogue struct {
	pricing          []pricingdomain.Pricing
	vendors          []pricingdomain.PricingVendor
	revision         string
	endpoints        map[string]common.EndpointInfo
	builtAt          time.Time
	generation       uint64
	pluginGeneration uint64
}

var publishedPricing atomic.Pointer[publishedPricingCatalogue]

var pricingInputsGeneration atomic.Uint64

var pricingPublicationLock sync.Mutex

var pricingBuiltGeneration uint64

var pricingBuiltPluginGeneration uint64

const pricingBuildRetryDelay = 30 * time.Second

var pricingAttemptGeneration atomic.Uint64

var pricingAttemptPluginGeneration atomic.Uint64

var pricingRetryAfter atomic.Int64

var pricingRefreshRunning atomic.Bool

func pricingBuildDue() bool {
	return pricingAttemptGeneration.Load() != pricingInputsGeneration.Load() || pricingAttemptPluginGeneration.Load() != jsplugin.DefaultRegistry.Generation().Number || time.Now().UnixNano() >= pricingRetryAfter.Load()
}

func GetPricingCatalogueSnapshot(read func() (read_catalogue.Snapshot, error)) ([]pricingdomain.Pricing, []pricingdomain.PricingVendor, string, error) {
	snapshot, err := getPublishedPricingCatalogue(read)
	if err != nil {
		return nil, nil, "", err
	}
	return snapshot.pricing, snapshot.vendors, snapshot.revision, nil
}

func GetPricingCatalogueSnapshotWithEndpoints(read func() (read_catalogue.Snapshot, error)) ([]pricingdomain.Pricing, []pricingdomain.PricingVendor, map[string]common.EndpointInfo, string, error) {
	snapshot, err := getPublishedPricingCatalogue(read)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return snapshot.pricing, snapshot.vendors, snapshot.endpoints, snapshot.revision, nil
}

func getPublishedPricingCatalogue(read func() (read_catalogue.Snapshot, error)) (*publishedPricingCatalogue, error) {
	snapshot, err := read()
	if err != nil {
		return nil, err
	}
	return snapshot.(*publishedPricingCatalogue), nil
}

func (p *publishedPricingCatalogue) Generation() uint64 { return p.generation }

func (p *publishedPricingCatalogue) PluginGeneration() uint64 { return p.pluginGeneration }

type PricingReadState struct{}

func (PricingReadState) Snapshot() read_catalogue.Snapshot {
	p := publishedPricing.Load()
	if p == nil {
		return nil
	}
	return p
}

func (PricingReadState) BuildDue() bool { return pricingBuildDue() }

func (PricingReadState) LockBuild() { updatePricingLock.Lock() }

func (PricingReadState) UnlockBuild() { updatePricingLock.Unlock() }

func (PricingReadState) LockEndpoints() { modelSupportEndpointsLock.Lock() }

func (PricingReadState) UnlockEndpoints() { modelSupportEndpointsLock.Unlock() }

func (PricingReadState) Generation() uint64 { return pricingInputsGeneration.Load() }

func (PricingReadState) PluginGeneration() uint64 {
	return jsplugin.DefaultRegistry.Generation().Number
}

func (PricingReadState) StartRefresh() bool { return pricingRefreshRunning.CompareAndSwap(false, true) }

func (PricingReadState) EndRefresh() { pricingRefreshRunning.Store(false) }
