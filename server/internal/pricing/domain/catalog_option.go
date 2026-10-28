package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

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

const PricingCatalogOptionKey = "PricingCatalog"
const pricingCatalogMaxBytes = 8 << 20
const pricingCatalogMaxEntries = 2048

func PricingCatalogSourceFingerprint(deps Capabilities, channel *BuildChannel, upstream string, endpoint constant.EndpointType) string {
	var mapping map[string]string
	if raw := strings.TrimSpace(channel.GetModelMapping()); raw != "" {
		if err := deps.Codec.UnmarshalJsonStr(raw, &mapping); err != nil {
			return ""
		}
	}
	routed := false
	for _, name := range channel.GetModels() {
		target, cyclic := FollowChannelModelMapping(mapping, name)
		if !cyclic && target == upstream {
			routed = true
			break
		}
	}
	return PricingCatalogFingerprint(deps, channel, upstream, endpoint, []any{"route-v2", routed})
}
func PricingCatalogFingerprint(deps Capabilities, channel *BuildChannel, upstream string, endpoint constant.EndpointType, routeIdentity []any) string {
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.GetChannelBaseURL(channel.Type)
	}
	basis := []any{channel.Id, channel.Type, channel.Status, strings.TrimRight(baseURL, "/"), channel.Group}
	basis = append(basis, routeIdentity...)
	basis = append(basis, channel.Key, channel.Setting, channel.OtherSettings, channel.ParamOverride, channel.HeaderOverride, upstream, endpoint)
	data, err := deps.Codec.Marshal(basis)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
func PricingCatalogSourceMatches(deps Capabilities, entry PricingCatalogEntry, channel *BuildChannel) bool {
	current := PricingCatalogSourceFingerprint(deps, channel, entry.UpstreamModel, entry.Endpoint)
	if current == "" {
		return false
	}
	if entry.Fingerprint == current {
		return true
	}

	legacy := PricingCatalogFingerprint(deps, channel, entry.UpstreamModel, entry.Endpoint, []any{channel.Models, channel.GetModelMapping()})
	return entry.Fingerprint == legacy
}
func PricingCatalogEntryKey(entry PricingCatalogEntry) string {
	return fmt.Sprintf("%d:%s:%s", entry.ChannelID, entry.Endpoint, entry.UpstreamModel)
}
func DecodePricingCatalog(deps Capabilities, value string) (PricingCatalogSnapshot, error) {
	var snapshot PricingCatalogSnapshot
	if len(value) > pricingCatalogMaxBytes {
		return snapshot, fmt.Errorf("pricing catalog exceeds size limit")
	}
	if err := deps.Codec.DecodeCatalogSnapshot(value, &snapshot); err != nil {
		return snapshot, fmt.Errorf("invalid pricing catalog: %w", err)
	}
	if snapshot.Version != 1 || snapshot.Entries == nil || len(snapshot.Entries) > pricingCatalogMaxEntries {
		return snapshot, fmt.Errorf("invalid pricing catalog version or entries")
	}
	for key, entry := range snapshot.Entries {
		fingerprint, err := hex.DecodeString(entry.Fingerprint)
		if entry.ChannelID <= 0 || entry.UpstreamModel == "" || entry.Endpoint == "" || !strings.HasPrefix(entry.Source, "https://") || err != nil || len(fingerprint) != sha256.Size || key != PricingCatalogEntryKey(entry) {
			return snapshot, fmt.Errorf("invalid pricing catalog source identity")
		}
		catalog := TariffCatalog{FetchedAt: entry.FetchedAt, Models: map[string][]CatalogOffer{entry.UpstreamModel: entry.Offers}}
		if err := deps.Catalog.ValidatePriceCatalog(catalog, []string{entry.UpstreamModel}); err != nil {
			return snapshot, err
		}
	}
	return snapshot, nil
}
func PricingCatalogSemanticallyEqual(left, right PricingCatalogSnapshot) bool {
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
