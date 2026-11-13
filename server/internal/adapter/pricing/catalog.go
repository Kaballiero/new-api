package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

type CatalogClient struct{ Client *catalogsync.Client }

// UpstreamCatalogModelsCache keeps the upstream models of the last upstream
// catalog revision in the process.
type UpstreamCatalogModelsCache struct {
	mutex    sync.Mutex
	revision string
	models   channeldomain.UpstreamModels
}

func (cache *UpstreamCatalogModelsCache) Load(revision string) (channeldomain.UpstreamModels, bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	return cache.models, cache.revision == revision
}
func (cache *UpstreamCatalogModelsCache) Store(revision string, models channeldomain.UpstreamModels) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.revision, cache.models = revision, models
}

// UpstreamCatalogDocumentsCache keeps the decoded stored documents of the
// upstream catalog in the process by hash.
type UpstreamCatalogDocumentsCache struct {
	mutex     sync.Mutex
	documents map[string]*domain.SourceDocument
}

func (cache *UpstreamCatalogDocumentsCache) Load(hash string) (*domain.SourceDocument, bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	document, cached := cache.documents[hash]
	return document, cached
}
func (cache *UpstreamCatalogDocumentsCache) Store(hash string, document *domain.SourceDocument) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.documents == nil {
		cache.documents = make(map[string]*domain.SourceDocument)
	}
	cache.documents[hash] = document
}

// Retain releases the documents of all other hashes. The cache is rebuilt
// rather than trimmed, because a Go map does not shrink.
func (cache *UpstreamCatalogDocumentsCache) Retain(hashes []string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	retained := make(map[string]*domain.SourceDocument, len(hashes))
	for _, hash := range hashes {
		if document, cached := cache.documents[hash]; cached {
			retained[hash] = document
		}
	}
	cache.documents = retained
}

func (c CatalogClient) Manifest(ctx context.Context) (domain.SourceManifest, error) {
	value, err := c.Client.Manifest(ctx)
	return ToSourceManifest(value), err
}
func (c CatalogClient) Index(ctx context.Context) (domain.SourceIndex, error) {
	value, err := c.Client.Index(ctx)
	if err != nil {
		return domain.SourceIndex{}, err
	}
	result := domain.SourceIndex{Revision: value.Revision, Models: make([]domain.SourceIndexModel, len(value.Models))}
	for i, model := range value.Models {
		result.Models[i] = domain.SourceIndexModel{ID: model.ID, Hash: model.Hash, Type: model.Type, Modalities: ToSourceModalities(model.Modalities), Endpoints: model.Endpoints}
		if model.Alias != nil {
			alias := ToSourceAlias(*model.Alias)
			result.Models[i].Alias = &alias
		}
	}
	return result, nil
}
func (c CatalogClient) Document(ctx context.Context, ref domain.SourceReference) ([]byte, *domain.SourceDocument, error) {
	raw, value, err := c.Client.Document(ctx, nativeSourceReference(ref))
	if value == nil {
		return raw, nil, err
	}
	result := ToSourceDocument(*value)
	return raw, &result, err
}

// PricingCatalogSnapshot and PricingCatalogEntry retain the persisted wire decoder's
// original field types and names, including encoding/json error text.
type PricingCatalogSnapshot struct {
	Version int                            `json:"version"`
	Entries map[string]PricingCatalogEntry `json:"entries"`
}
type PricingCatalogEntry struct {
	ChannelID     int                     `json:"channel_id"`
	UpstreamModel string                  `json:"upstream_model"`
	Endpoint      constant.EndpointType   `json:"endpoint"`
	Source        string                  `json:"source"`
	FetchedAt     string                  `json:"fetched_at"`
	Fingerprint   string                  `json:"fingerprint"`
	Offers        []jsplugin.CatalogOffer `json:"offers"`
}

func decodeCatalogSnapshot(raw string, result *domain.PricingCatalogSnapshot) error {
	var native PricingCatalogSnapshot
	err := common.UnmarshalJsonStr(raw, &native)
	// Keep the original domain container type facts in errors. Nested SDK types
	// already retain their original identities through the wire decoder above.
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		switch typeError.Type {
		case reflect.TypeFor[PricingCatalogSnapshot]():
			typeError.Type = reflect.TypeFor[domain.PricingCatalogSnapshot]()
		case reflect.TypeFor[map[string]PricingCatalogEntry]():
			typeError.Type = reflect.TypeFor[map[string]domain.PricingCatalogEntry]()
		case reflect.TypeFor[PricingCatalogEntry]():
			typeError.Type = reflect.TypeFor[domain.PricingCatalogEntry]()
		}
	}
	result.Version = native.Version
	if native.Entries != nil {
		result.Entries = make(map[string]domain.PricingCatalogEntry, len(native.Entries))
		for key, value := range native.Entries {
			entry := domain.PricingCatalogEntry{
				ChannelID: value.ChannelID, UpstreamModel: value.UpstreamModel, Endpoint: value.Endpoint,
				Source: value.Source, FetchedAt: value.FetchedAt, Fingerprint: value.Fingerprint,
			}
			if value.Offers != nil {
				entry.Offers = make([]domain.CatalogOffer, len(value.Offers))
				for i, offer := range value.Offers {
					entry.Offers[i] = ToTariffOffer(offer)
				}
			}
			result.Entries[key] = entry
		}
	}
	return err
}
