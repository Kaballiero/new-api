package model

import (
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/read_catalogue"

	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

var pricingCatalogueRevision string
var pricingCatalogueRevisionError error

type publishedPricingCatalogue struct {
	pricing          []Pricing
	vendors          []PricingVendor
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

func GetPricingCatalogueSnapshot() ([]Pricing, []PricingVendor, string, error) {
	snapshot, err := getPublishedPricingCatalogue()
	if err != nil {
		return nil, nil, "", err
	}
	return snapshot.pricing, snapshot.vendors, snapshot.revision, nil
}

func GetPricingCatalogueSnapshotWithEndpoints() ([]Pricing, []PricingVendor, map[string]common.EndpointInfo, string, error) {
	snapshot, err := getPublishedPricingCatalogue()
	if err != nil {
		return nil, nil, nil, "", err
	}
	return snapshot.pricing, snapshot.vendors, snapshot.endpoints, snapshot.revision, nil
}

func getPublishedPricingCatalogue() (*publishedPricingCatalogue, error) {
	snapshot, err := read_catalogue.NewUsecase(pricingReadState{}, rebuildPricingCatalogueLocked).Execute()
	if err != nil {
		return nil, err
	}
	return snapshot.(*publishedPricingCatalogue), nil
}
func (p *publishedPricingCatalogue) Generation() uint64       { return p.generation }
func (p *publishedPricingCatalogue) PluginGeneration() uint64 { return p.pluginGeneration }

type pricingReadState struct{}

func (pricingReadState) Snapshot() read_catalogue.Snapshot {
	p := publishedPricing.Load()
	if p == nil {
		return nil
	}
	return p
}
func (pricingReadState) BuildDue() bool     { return pricingBuildDue() }
func (pricingReadState) LockBuild()         { updatePricingLock.Lock() }
func (pricingReadState) UnlockBuild()       { updatePricingLock.Unlock() }
func (pricingReadState) LockEndpoints()     { modelSupportEndpointsLock.Lock() }
func (pricingReadState) UnlockEndpoints()   { modelSupportEndpointsLock.Unlock() }
func (pricingReadState) Generation() uint64 { return pricingInputsGeneration.Load() }
func (pricingReadState) PluginGeneration() uint64 {
	return jsplugin.DefaultRegistry.Generation().Number
}
func (pricingReadState) StartRefresh() bool { return pricingRefreshRunning.CompareAndSwap(false, true) }
func (pricingReadState) EndRefresh()        { pricingRefreshRunning.Store(false) }

func pricingCatalogueFingerprint(pricing []Pricing, vendors []PricingVendor) (string, error) {
	return pricingdomain.PricingCatalogueFingerprint(pricing, vendors)
}
