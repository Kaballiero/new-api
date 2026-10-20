package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
	snapshot := publishedPricing.Load()
	if snapshot == nil {
		// Cold startup initializes honestly; subsequent reads never wait for builds.
		updatePricingLock.Lock()
		if publishedPricing.Load() == nil && pricingBuildDue() {
			modelSupportEndpointsLock.Lock()
			rebuildPricingCatalogueLocked()
			modelSupportEndpointsLock.Unlock()
		}
		updatePricingLock.Unlock()
		snapshot = publishedPricing.Load()
	} else if snapshot.generation != pricingInputsGeneration.Load() || snapshot.pluginGeneration != jsplugin.DefaultRegistry.Generation().Number {
		if pricingBuildDue() && pricingRefreshRunning.CompareAndSwap(false, true) {
			go func() {
				defer pricingRefreshRunning.Store(false)
				updatePricingLock.Lock()
				defer updatePricingLock.Unlock()
				current := publishedPricing.Load()
				if current != nil && current.generation == pricingInputsGeneration.Load() && current.pluginGeneration == jsplugin.DefaultRegistry.Generation().Number {
					return
				}
				if !pricingBuildDue() {
					return
				}
				modelSupportEndpointsLock.Lock()
				defer modelSupportEndpointsLock.Unlock()
				rebuildPricingCatalogueLocked()
			}()
		}
	}
	if snapshot == nil {
		return nil, errors.New("pricing catalogue unavailable")
	}
	return snapshot, nil
}

func pricingCatalogueFingerprint(pricing []Pricing, vendors []PricingVendor) (string, error) {
	rows := make([]string, 0, len(pricing))
	for _, item := range pricing {
		groups := append([]string{}, item.EnableGroup...)
		slices.Sort(groups)
		endpoints := append([]constant.EndpointType{}, item.SupportedEndpointTypes...)
		slices.Sort(endpoints)
		offers, err := pricingFingerprintOffers(item.CatalogOffersByGroup)
		if err != nil {
			return "", err
		}
		publicOffers, err := pricingFingerprintOffers(item.PublicCatalogOffersByGroup)
		if err != nil {
			return "", err
		}
		canonical := make(map[string]string, len(item.ConsumerCanonicalModelsByGroup))
		for group, base := range item.ConsumerCanonicalModelsByGroup {
			canonical[group] = base
		}
		upstreamRoutes := make(map[string][]string, len(item.UpstreamCatalogRoutesByGroup))
		for group, routes := range item.UpstreamCatalogRoutesByGroup {
			for _, route := range routes {
				policy := ""
				if route.ParamOverride != nil {
					policy = CatalogHash([]byte(*route.ParamOverride))
				}
				upstreamRoutes[group] = append(upstreamRoutes[group], fmt.Sprintf("semantic-v2:%d:%s:%s:%s:%s:%s:%s:%t", route.ChannelID, route.ModelID, route.DocumentHash, route.Operation, route.Mode, route.Endpoint, policy, route.ServiceTierEnabled))
			}
			slices.Sort(upstreamRoutes[group])
		}
		row := struct {
			ModelName, Description, Icon, Tags, OwnerBy, DisplayName string
			UpstreamRoutes                                           map[string][]string
			VendorID                                                 int
			Groups                                                   []string
			Endpoints                                                []constant.EndpointType
			Offers                                                   map[string][]string
			CanonicalGroups                                          map[string]string
			PublicOffers                                             map[string][]string
		}{item.ModelName, item.Description, item.Icon, item.Tags, item.OwnerBy, item.DisplayName, upstreamRoutes, item.VendorID, groups, endpoints, offers, canonical, publicOffers}
		value, err := common.Marshal(row)
		if err != nil {
			return "", err
		}
		rows = append(rows, string(value))
	}
	slices.Sort(rows)
	creators := append([]PricingVendor{}, vendors...)
	slices.SortFunc(creators, func(left, right PricingVendor) int { return left.ID - right.ID })
	value, err := common.Marshal(struct {
		Rows    []string
		Vendors []PricingVendor
	}{rows, creators})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:]), nil
}

func pricingFingerprintOffers(byGroup map[string][]CatalogOffer) (map[string][]string, error) {
	offers := make(map[string][]string, len(byGroup))
	for group, candidates := range byGroup {
		offers[group] = make([]string, 0, len(candidates))
		for _, offer := range candidates {
			offer.Prices = append([]CatalogPrice{}, offer.Prices...)
			slices.SortFunc(offer.Prices, comparePricingFingerprintPrices)
			value, err := common.Marshal(offer)
			if err != nil {
				return nil, err
			}
			offers[group] = append(offers[group], string(value))
		}
		slices.Sort(offers[group])
	}
	return offers, nil
}

func comparePricingFingerprintPrices(left, right CatalogPrice) int {
	if left.Component < right.Component {
		return -1
	}
	if left.Component > right.Component {
		return 1
	}
	if left.Unit < right.Unit {
		return -1
	}
	if left.Unit > right.Unit {
		return 1
	}
	return 0
}
