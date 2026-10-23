package model

import (
	"context"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/publish_native_catalogue"

	"fmt"
	"gorm.io/gorm"

	"reflect"

	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

const PricingCatalogOptionKey = "PricingCatalog"
const pricingCatalogMaxBytes = 8 << 20

type PricingCatalogSnapshot = pricingdomain.PricingCatalogSnapshot

type PricingCatalogEntry = pricingdomain.PricingCatalogEntry

var pricingCatalogState atomic.Pointer[PricingCatalogSnapshot]

func PricingCatalogSourceFingerprint(channel *Channel, upstream string, endpoint constant.EndpointType) string {
	return pricingdomain.PricingCatalogSourceFingerprint(pricingBuildChannel(channel), upstream, endpoint)
}

func pricingCatalogFingerprint(channel *Channel, upstream string, endpoint constant.EndpointType, routeIdentity []any) string {
	return pricingdomain.PricingCatalogFingerprint(pricingBuildChannel(channel), upstream, endpoint, routeIdentity)
}

func pricingCatalogSourceMatches(entry PricingCatalogEntry, channel *Channel) bool {
	return pricingdomain.PricingCatalogSourceMatches(entry, pricingBuildChannel(channel))
}

func pricingCatalogEntryKey(entry PricingCatalogEntry) string {
	return pricingdomain.PricingCatalogEntryKey(entry)
}

func decodePricingCatalog(value string) (PricingCatalogSnapshot, error) {
	return pricingdomain.DecodePricingCatalog(value)
}

func loadPricingCatalog(snapshot PricingCatalogSnapshot) {
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

func GetPricingCatalogEntry(channel *Channel, upstream string, endpoint constant.EndpointType) (PricingCatalogEntry, bool) {
	snapshot := pricingCatalogState.Load()
	if snapshot == nil {
		return PricingCatalogEntry{}, false
	}
	identity := PricingCatalogEntry{ChannelID: channel.Id, UpstreamModel: upstream, Endpoint: endpoint}
	entry, known := snapshot.Entries[pricingCatalogEntryKey(identity)]
	if !known || channel.Status != common.ChannelStatusEnabled || !pricingCatalogSourceMatches(entry, channel) {
		return PricingCatalogEntry{}, false
	}
	return entry, true
}

func PublishPricingCatalogEntry(entry PricingCatalogEntry) error {
	return PublishPricingCatalogEntryContext(context.Background(), entry)
}

func PublishPricingCatalogEntryContext(ctx context.Context, entry PricingCatalogEntry) error {
	published, err := publishPricingCatalogContext(ctx, []PricingCatalogEntry{entry}, false)
	if err == nil && published == 0 {
		return fmt.Errorf("pricing catalog source changed before publication")
	}
	return err
}

// PublishPricingCatalogContext commits a refresh once, fencing source identity
// with write locks held until the option transaction commits. The harmless
// channel update also acquires SQLite's writer lock before reading identities.
func PublishPricingCatalogContext(ctx context.Context, entries []PricingCatalogEntry) (int, error) {
	return publishPricingCatalogContext(ctx, entries, true)
}

func publishPricingCatalogContext(ctx context.Context, entries []PricingCatalogEntry, allowChangedSources bool) (int, error) {
	return publish_native_catalogue.NewUsecase(store.NewNative(store.NativeEnvironment{DB: func() *gorm.DB { return DB }, LockRows: LockForUpdate, Lock: func() func() { groupRatioOptionMutex.Lock(); return groupRatioOptionMutex.Unlock }, Snapshot: pricingCatalogState.Load, Publish: updateOptionMap})).Execute(ctx, entries, allowChangedSources)
}

func pricingCatalogSemanticallyEqual(left, right PricingCatalogSnapshot) bool {
	return pricingdomain.PricingCatalogSemanticallyEqual(left, right)
}
