package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"gorm.io/gorm"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

const PricingCatalogOptionKey = "PricingCatalog"
const pricingCatalogMaxBytes = 8 << 20
const pricingCatalogMaxEntries = 2048

type PricingCatalogSnapshot struct {
	Version int                            `json:"version"`
	Entries map[string]PricingCatalogEntry `json:"entries"`
}

type PricingCatalogEntry struct {
	ChannelID     int                   `json:"channel_id"`
	UpstreamModel string                `json:"upstream_model"`
	Endpoint      constant.EndpointType `json:"endpoint"`
	Source        string                `json:"source"`
	FetchedAt     string                `json:"fetched_at"`
	Fingerprint   string                `json:"fingerprint"`
	Offers        []CatalogOffer        `json:"offers"`
}

var pricingCatalogState atomic.Pointer[PricingCatalogSnapshot]

func PricingCatalogSourceFingerprint(channel *Channel, upstream string, endpoint constant.EndpointType) string {
	var mapping map[string]string
	if raw := strings.TrimSpace(channel.GetModelMapping()); raw != "" {
		if err := common.UnmarshalJsonStr(raw, &mapping); err != nil {
			return ""
		}
	}
	routed := false
	for _, name := range channel.GetModels() {
		target, cyclic := followChannelModelMapping(mapping, name)
		if !cyclic && target == upstream {
			routed = true
			break
		}
	}
	return pricingCatalogFingerprint(channel, upstream, endpoint, []any{"route-v2", routed})
}

func pricingCatalogFingerprint(channel *Channel, upstream string, endpoint constant.EndpointType, routeIdentity []any) string {
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.GetChannelBaseURL(channel.Type)
	}
	basis := []any{channel.Id, channel.Type, channel.Status, strings.TrimRight(baseURL, "/"), channel.Group}
	basis = append(basis, routeIdentity...)
	basis = append(basis, channel.Key, channel.Setting, channel.OtherSettings, channel.ParamOverride, channel.HeaderOverride, upstream, endpoint)
	data, err := common.Marshal(basis)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func pricingCatalogSourceMatches(entry PricingCatalogEntry, channel *Channel) bool {
	current := PricingCatalogSourceFingerprint(channel, entry.UpstreamModel, entry.Endpoint)
	if current == "" {
		return false
	}
	if entry.Fingerprint == current {
		return true
	}
	// Retain existing snapshots only when their original full channel identity
	// still matches; never reinterpret a stale legacy fingerprint as valid.
	legacy := pricingCatalogFingerprint(channel, entry.UpstreamModel, entry.Endpoint, []any{channel.Models, channel.GetModelMapping()})
	return entry.Fingerprint == legacy
}

func pricingCatalogEntryKey(entry PricingCatalogEntry) string {
	return fmt.Sprintf("%d:%s:%s", entry.ChannelID, entry.Endpoint, entry.UpstreamModel)
}

func decodePricingCatalog(value string) (PricingCatalogSnapshot, error) {
	var snapshot PricingCatalogSnapshot
	if len(value) > pricingCatalogMaxBytes {
		return snapshot, fmt.Errorf("pricing catalog exceeds size limit")
	}
	if err := common.UnmarshalJsonStr(value, &snapshot); err != nil {
		return snapshot, fmt.Errorf("invalid pricing catalog: %w", err)
	}
	if snapshot.Version != 1 || snapshot.Entries == nil || len(snapshot.Entries) > pricingCatalogMaxEntries {
		return snapshot, fmt.Errorf("invalid pricing catalog version or entries")
	}
	for key, entry := range snapshot.Entries {
		fingerprint, err := hex.DecodeString(entry.Fingerprint)
		if entry.ChannelID <= 0 || entry.UpstreamModel == "" || entry.Endpoint == "" || !strings.HasPrefix(entry.Source, "https://") || err != nil || len(fingerprint) != sha256.Size || key != pricingCatalogEntryKey(entry) {
			return snapshot, fmt.Errorf("invalid pricing catalog source identity")
		}
		catalog := jsplugin.PriceCatalog{FetchedAt: entry.FetchedAt, Models: map[string][]CatalogOffer{entry.UpstreamModel: entry.Offers}}
		if err := catalog.Validate([]string{entry.UpstreamModel}); err != nil {
			return snapshot, err
		}
	}
	return snapshot, nil
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
	groupRatioOptionMutex.Lock()
	defer groupRatioOptionMutex.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	snapshot := PricingCatalogSnapshot{Version: 1, Entries: make(map[string]PricingCatalogEntry)}
	if current := pricingCatalogState.Load(); current != nil {
		snapshot.Entries = maps.Clone(current.Entries)
	}
	var data []byte
	published := 0
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		option := Option{Key: PricingCatalogOptionKey}
		if err := tx.FirstOrCreate(&option, Option{Key: PricingCatalogOptionKey}).Error; err != nil {
			return err
		}
		if err := tx.Model(&option).UpdateColumn("value", gorm.Expr("value")).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&option, Option{Key: PricingCatalogOptionKey}).Error; err != nil {
			return err
		}
		if current, err := decodePricingCatalog(option.Value); err == nil {
			maps.Copy(snapshot.Entries, current.Entries)
		}
		ids := map[int]bool{}
		for _, entry := range snapshot.Entries {
			ids[entry.ChannelID] = true
		}
		for _, entry := range entries {
			ids[entry.ChannelID] = true
		}
		channels := make(map[int]*Channel)
		for _, id := range slices.Sorted(maps.Keys(ids)) {
			result := tx.Model(&Channel{}).Where("id = ?", id).UpdateColumn("id", gorm.Expr("id"))
			if result.Error != nil {
				return result.Error
			}
			channel := &Channel{}
			if err := lockForUpdate(tx).First(channel, "id = ?", id).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					continue
				}
				return err
			}
			channels[id] = channel
		}
		for key, entry := range snapshot.Entries {
			channel := channels[entry.ChannelID]
			if channel == nil || channel.Status != common.ChannelStatusEnabled || !pricingCatalogSourceMatches(entry, channel) {
				delete(snapshot.Entries, key)
			}
		}
		for _, entry := range entries {
			channel := channels[entry.ChannelID]
			if channel != nil && channel.Status == common.ChannelStatusEnabled && pricingCatalogSourceMatches(entry, channel) {
				snapshot.Entries[pricingCatalogEntryKey(entry)] = entry
				published++
			} else if !allowChangedSources {
				return fmt.Errorf("pricing catalog source changed before publication")
			}
		}

		var err error
		data, err = common.Marshal(snapshot)
		if err != nil {
			return err
		}
		if _, err = decodePricingCatalog(string(data)); err != nil {
			return err
		}
		return tx.Model(&option).Update("value", string(data)).Error
	})
	if err != nil {
		return 0, err
	}
	return published, updateOptionMap(PricingCatalogOptionKey, string(data))
}

func pricingCatalogSemanticallyEqual(left, right PricingCatalogSnapshot) bool {
	if len(left.Entries) != len(right.Entries) {
		return false
	}
	for key, entry := range left.Entries {
		other, ok := right.Entries[key]
		if !ok {
			return false
		}
		entry.FetchedAt = ""
		other.FetchedAt = ""
		if !reflect.DeepEqual(entry, other) {
			return false
		}
	}
	return true
}
