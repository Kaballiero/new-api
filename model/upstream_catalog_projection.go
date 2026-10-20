package model

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"gorm.io/gorm"
)

const CatalogAPIURLEnv = "PRICING_CATALOG_API_URL"

type UpstreamCatalogRoute struct {
	Operation    string
	Mode         string
	Endpoint     constant.EndpointType
	ModelID      string
	DocumentHash string
	Document     *catalogsync.Document
	// Overrides are private projection inputs, never consumer response fields.
	ChannelID          int     `json:"-"`
	ParamOverride      *string `json:"-"`
	ServiceTierEnabled bool    `json:"-"`
}

func ConfiguredCatalogSource() (CatalogSource, bool, error) {
	configured := strings.TrimSpace(os.Getenv(CatalogAPIURLEnv))
	if configured == "" {
		return CatalogSource{}, false, nil
	}
	base, err := catalogsync.NormalizeBaseURL(configured)
	if err != nil {
		return CatalogSource{}, true, err
	}
	return CatalogSource{ID: CatalogHash([]byte("openrouter\n" + base)), Kind: "openrouter", BaseURL: base}, true, nil
}

// Only actual OpenRouter channels can consume the initial acquisition source.
// A compatible protocol or an identical model name does not establish identity.
func IsOpenRouterCatalogChannel(channel *Channel) bool {
	if channel == nil || channel.Type != constant.ChannelTypeOpenRouter {
		return false
	}
	base := channel.GetBaseURL()
	if base == "" {
		base = constant.GetChannelBaseURL(channel.Type)
	}
	return strings.TrimRight(base, "/") == "https://openrouter.ai/api"
}

func loadConfiguredCatalog(ctx context.Context) (map[string]*catalogsync.Document, map[string]string, bool, error) {
	source, enabled, err := ConfiguredCatalogSource()
	if err != nil || !enabled {
		return nil, nil, enabled, err
	}
	var head CatalogSource
	if err := DB.WithContext(ctx).Where("id = ?", source.ID).Take(&head).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return map[string]*catalogsync.Document{}, map[string]string{}, true, nil
		}
		return nil, nil, true, err
	}
	if head.Revision == "" {
		return map[string]*catalogsync.Document{}, map[string]string{}, true, nil
	}
	selection, err := ReadCatalogSelection(ctx, source.ID, head.Revision)
	if err != nil {
		return nil, nil, true, err
	}
	hashes := make([]string, 0, len(selection.Models))
	for _, ref := range selection.Models {
		hashes = append(hashes, ref.Hash)
	}
	stored, err := GetCatalogDocuments(ctx, source.ID, hashes)
	if err != nil {
		return nil, nil, true, err
	}
	models := make(map[string]*catalogsync.Document, len(selection.Models))
	byID := make(map[string]string, len(selection.Models))
	for _, ref := range selection.Models {
		row, exists := stored[ref.Hash]
		if !exists {
			return nil, nil, true, errors.New("published catalog document missing")
		}
		doc, err := catalogsync.DecodeDocument([]byte(row.Payload), catalogsync.Reference{ID: ref.ID, Hash: ref.Hash})
		if err != nil {
			return nil, nil, true, err
		}
		models[ref.ID] = doc
		byID[ref.ID] = ref.Hash
	}
	for id := range models {
		if _, err := catalogsync.ResolveAlias(models, id); err != nil {
			return nil, nil, true, err
		}
	}
	return models, byID, true, nil
}

func persistedCatalogRevision(ctx context.Context) (string, error) {
	source, enabled, err := ConfiguredCatalogSource()
	if err != nil || !enabled {
		return "", err
	}
	var head CatalogSource
	if err := DB.WithContext(ctx).Select("id", "revision").Where("id = ?", source.ID).Take(&head).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return source.ID, nil
		}
		return "", err
	}
	return source.ID + ":" + head.Revision, nil
}

func (p Pricing) UpstreamCatalogRoutesForGroup(group string) []UpstreamCatalogRoute {
	return append(slices.Clone(p.UpstreamCatalogRoutesByGroup["all"]), p.UpstreamCatalogRoutesByGroup[group]...)
}
