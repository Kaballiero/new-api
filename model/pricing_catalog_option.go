package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
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
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.GetChannelBaseURL(channel.Type)
	}
	basis := []any{channel.Id, channel.Type, channel.Status, strings.TrimRight(baseURL, "/"), channel.Group, channel.Models, channel.GetModelMapping(), channel.Key, channel.Setting, channel.OtherSettings, channel.ParamOverride, channel.HeaderOverride, upstream, endpoint}
	data, err := common.Marshal(basis)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
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
	pricingCatalogState.Store(&snapshot)
	InvalidatePricingCache()
	pricingRevision.Add(1)
}

func GetPricingCatalogEntry(channel *Channel, upstream string, endpoint constant.EndpointType) (PricingCatalogEntry, bool) {
	snapshot := pricingCatalogState.Load()
	if snapshot == nil {
		return PricingCatalogEntry{}, false
	}
	identity := PricingCatalogEntry{ChannelID: channel.Id, UpstreamModel: upstream, Endpoint: endpoint}
	entry, known := snapshot.Entries[pricingCatalogEntryKey(identity)]
	if !known || channel.Status != common.ChannelStatusEnabled || entry.Fingerprint != PricingCatalogSourceFingerprint(channel, upstream, endpoint) {
		return PricingCatalogEntry{}, false
	}
	return entry, true
}

func PublishPricingCatalogEntry(entry PricingCatalogEntry) error {
	return PublishPricingCatalogEntryContext(context.Background(), entry)
}

func PublishPricingCatalogEntryContext(ctx context.Context, entry PricingCatalogEntry) error {
	groupRatioOptionMutex.Lock()
	defer groupRatioOptionMutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	channel := &Channel{}
	if err := DB.WithContext(ctx).First(channel, "id = ?", entry.ChannelID).Error; err != nil {
		return err
	}
	if channel.Status != common.ChannelStatusEnabled || entry.Fingerprint != PricingCatalogSourceFingerprint(channel, entry.UpstreamModel, entry.Endpoint) {
		return fmt.Errorf("pricing catalog source changed before publication")
	}
	snapshot := PricingCatalogSnapshot{Version: 1, Entries: make(map[string]PricingCatalogEntry)}
	if current := pricingCatalogState.Load(); current != nil {
		snapshot.Entries = maps.Clone(current.Entries)
	}
	snapshot.Entries[pricingCatalogEntryKey(entry)] = entry
	data, err := common.Marshal(snapshot)
	if err != nil {
		return err
	}
	return updateOptionsBulkLockedContext(ctx, map[string]string{PricingCatalogOptionKey: string(data)})
}
