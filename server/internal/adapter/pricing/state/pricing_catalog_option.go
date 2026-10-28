package pricingstate

import (
	"reflect"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	legacy "github.com/QuantumNous/new-api/model"
)

var pricingCatalogState atomic.Pointer[pricingdomain.PricingCatalogSnapshot]

func pricingCatalogFingerprint(channel *legacy.Channel, upstream string, endpoint constant.EndpointType, routeIdentity []any) string {
	return pricingdomain.PricingCatalogFingerprint(pricingCapabilities(), PricingBuildChannel(channel), upstream, endpoint, routeIdentity)
}

func pricingCatalogSourceMatches(entry pricingdomain.PricingCatalogEntry, channel *legacy.Channel) bool {
	return pricingdomain.PricingCatalogSourceMatches(pricingCapabilities(), entry, PricingBuildChannel(channel))
}

func pricingCatalogEntryKey(entry pricingdomain.PricingCatalogEntry) string {
	return pricingdomain.PricingCatalogEntryKey(entry)
}

func DecodePricingCatalog(value string) (pricingdomain.PricingCatalogSnapshot, error) {
	return pricingdomain.DecodePricingCatalog(pricingCapabilities(), value)
}

func LoadPricingCatalog(snapshot pricingdomain.PricingCatalogSnapshot) {
	previous := pricingCatalogState.Load()
	if previous != nil && reflect.DeepEqual(*previous, snapshot) {
		return
	}
	changed := previous == nil || !pricingCatalogSemanticallyEqual(*previous, snapshot)
	pricingPublicationLock.Lock()
	defer pricingPublicationLock.Unlock()
	pricingCatalogState.Store(&snapshot)
	if changed {
		pricingInputsGeneration.Add(1)
		pricingRevision.Add(1)
	}

}

func GetPricingCatalogEntry(channel *legacy.Channel, upstream string, endpoint constant.EndpointType) (pricingdomain.PricingCatalogEntry, bool) {
	snapshot := pricingCatalogState.Load()
	if snapshot == nil {
		return pricingdomain.PricingCatalogEntry{}, false
	}
	identity := pricingdomain.PricingCatalogEntry{ChannelID: channel.Id, UpstreamModel: upstream, Endpoint: endpoint}
	entry, known := snapshot.Entries[pricingCatalogEntryKey(identity)]
	if !known || channel.Status != common.ChannelStatusEnabled || !pricingCatalogSourceMatches(entry, channel) {
		return pricingdomain.PricingCatalogEntry{}, false
	}
	return entry, true
}

func pricingCatalogSemanticallyEqual(left, right pricingdomain.PricingCatalogSnapshot) bool {
	return pricingdomain.PricingCatalogSemanticallyEqual(left, right)
}

func NativeCatalogSnapshot() *pricingdomain.PricingCatalogSnapshot { return pricingCatalogState.Load() }
