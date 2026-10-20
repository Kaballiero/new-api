package model

import (
	"context"
	"errors"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/read_upstream_catalogue"
	"os"

	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"gorm.io/gorm"
)

const CatalogAPIURLEnv = "PRICING_CATALOG_API_URL"

// PricingEndpointSystemOne binds consumer rates to the existing native route.
// It is private to pricing and must not enter supported_endpoint_types.
const PricingEndpointSystemOne constant.EndpointType = "pricing_systemone"

type UpstreamCatalogRoute = pricingdomain.UpstreamCatalogRoute

// Catalog semantics describe the consumer operation, independently of the relay
// protocol. In particular, an image Chat request is not a text-generation offer.
func catalogConsumerProfile(document *catalogsync.Document) (pricingOperation, string) {
	v, modality := pricingdomain.CatalogConsumerProfile(document)
	return pricingOperation{v.Operation, v.Main, v.Basis}, modality
}

func catalogConsumerOperation(document *catalogsync.Document, upstream string, endpoint constant.EndpointType) pricingOperation {
	v := pricingdomain.CatalogConsumerOperation(document, upstream, endpoint)
	return pricingOperation{v.Operation, v.Main, v.Basis}
}

func CatalogOfferMode(offer catalogsync.Offer, upstream string) (string, bool) {
	return pricingdomain.CatalogOfferMode(offer, upstream)
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
	return pricingdomain.IsOpenRouterCatalogChannel(pricingBuildChannel(channel))
}

func loadConfiguredCatalog(ctx context.Context) (map[string]*catalogsync.Document, map[string]string, bool, error) {
	return read_upstream_catalogue.NewUsecase(NewImmutableCatalogPersistence(), func() (pricingdomain.CatalogSource, bool, error) {
		v, on, err := ConfiguredCatalogSource()
		return pricingdomain.CatalogSource(v), on, err
	}).Execute(ctx)
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
