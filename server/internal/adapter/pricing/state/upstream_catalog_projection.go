package pricingstate

import (
	"context"
	"errors"
	"os"
	"strings"

	pricingrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/record"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"

	"gorm.io/gorm"
)

const CatalogAPIURLEnv = "PRICING_CATALOG_API_URL"

// PricingEndpointSystemOne binds consumer rates to the existing native route.
// It is private to pricing and must not enter supported_endpoint_types.

// Catalog semantics describe the consumer operation, independently of the relay
// protocol. In particular, an image Chat request is not a text-generation offer.
func ConfiguredCatalogSource() (pricingrecord.CatalogSource, bool, error) {
	configured := strings.TrimSpace(os.Getenv(CatalogAPIURLEnv))
	if configured == "" {
		return pricingrecord.CatalogSource{}, false, nil
	}
	base, err := catalogsync.NormalizeBaseURL(configured)
	if err != nil {
		return pricingrecord.CatalogSource{}, true, err
	}
	return pricingrecord.CatalogSource{ID: CatalogHash([]byte("openrouter\n" + base)), Kind: "openrouter", BaseURL: base}, true, nil
}

// Only actual OpenRouter channels can consume the initial acquisition source.
// A compatible protocol or an identical model name does not establish identity.
func persistedCatalogRevision(ctx context.Context) (string, error) {
	source, enabled, err := ConfiguredCatalogSource()
	if err != nil || !enabled {
		return "", err
	}
	var head pricingrecord.CatalogSource
	if err := legacy.DB.WithContext(ctx).Select("id", "revision").Where("id = ?", source.ID).Take(&head).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return source.ID, nil
		}
		return "", err
	}
	return source.ID + ":" + head.Revision, nil
}
