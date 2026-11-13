package list_upstream_catalog_models

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Client interface {
	Manifest(context.Context) (domain.SourceManifest, error)
	Index(context.Context) (domain.SourceIndex, error)
	domain.CatalogDocumentClient
}

// Cache keeps the upstream models of the last catalog revision, so a
// revision's documents are read and its prices confirmed once.
type Cache interface {
	Load(revision string) (channeldomain.UpstreamModels, bool)
	Store(revision string, models channeldomain.UpstreamModels)
}

// addableRoute is the consumer operation a model added from the catalog may
// serve now, with the request path the catalog declares for it and the
// gateway endpoint that serves it.
type addableRoute struct {
	path     string
	endpoint constant.EndpointType
}

var addableRoutes = map[string]addableRoute{
	"text":             {"/api/v1/chat/completions", constant.EndpointTypeOpenAI},
	"embeddings":       {"/api/v1/embeddings", constant.EndpointTypeEmbeddings},
	"image_generation": {"/api/v1/images", constant.EndpointTypeImageGeneration},
}

// unaddableSuffixes mark catalog models that are not added to a channel: free
// variants and batch models.
var unaddableSuffixes = []string{":free", ":batch"}

type Usecase struct {
	deps       domain.Capabilities
	projection *domain.Projection
	store      domain.CatalogDocumentReader
	documents  domain.CatalogDocumentCache
	cache      Cache
	source     func() (domain.CatalogSource, bool, error)
	connect    func(domain.CatalogSource) (Client, error)
}

func NewUsecase(deps domain.Capabilities, projection *domain.Projection, store domain.CatalogDocumentReader, documents domain.CatalogDocumentCache, cache Cache, source func() (domain.CatalogSource, bool, error), connect func(domain.CatalogSource) (Client, error)) Usecase {
	return Usecase{deps: deps, projection: projection, store: store, documents: documents, cache: cache, source: source, connect: connect}
}

// Execute lists the models of the published upstream catalog as the upstream
// models of an OpenRouter channel. A model is addable when it is not an alias,
// free or batch model, serves an operation the gateway supports by its type
// and declared request path, and has a confirmed price of a synchronous offer.
func (u Usecase) Execute(ctx context.Context) (channeldomain.UpstreamModels, error) {
	source, enabled, err := u.source()
	if err != nil {
		return channeldomain.UpstreamModels{}, err
	}
	if !enabled {
		return channeldomain.UpstreamModels{}, errors.New("upstream catalog source is not configured")
	}
	client, err := u.connect(source)
	if err != nil {
		return channeldomain.UpstreamModels{}, err
	}
	// The manifest and the index are read by two requests; a publication
	// between them changes the revision, so the pair is read once more.
	var manifest domain.SourceManifest
	var index domain.SourceIndex
	for attempt := range 2 {
		if manifest, err = client.Manifest(ctx); err != nil {
			return channeldomain.UpstreamModels{}, err
		}
		if index, err = client.Index(ctx); err != nil {
			return channeldomain.UpstreamModels{}, err
		}
		if manifest.Revision == index.Revision {
			break
		}
		if attempt == 1 {
			return channeldomain.UpstreamModels{}, errors.New("upstream catalog index revision differs from manifest revision")
		}
	}

	if models, cached := u.cache.Load(manifest.Revision); cached {
		return models, nil
	}

	indexModels := make(map[string]domain.SourceIndexModel, len(index.Models))
	for _, model := range index.Models {
		indexModels[model.ID] = model
	}
	result := channeldomain.UpstreamModels{Listed: make([]string, 0, len(manifest.Models)), Addable: []string{}, Shortcuts: domain.OpenRouterShortcuts}
	candidates := []domain.SourceReference{}
	operations := map[string]domain.PricingOperation{}
	for _, ref := range manifest.Models {
		result.Listed = append(result.Listed, ref.ID)
		model, exists := indexModels[ref.ID]
		if !exists || model.Hash != ref.Hash {
			return channeldomain.UpstreamModels{}, errors.New("upstream catalog index does not match manifest")
		}
		if model.Alias != nil || slices.ContainsFunc(unaddableSuffixes, func(suffix string) bool { return strings.HasSuffix(model.ID, suffix) }) {
			continue
		}
		// The consumer profile reads only the type and the output modalities,
		// which the index carries unchanged.
		profile, _ := domain.CatalogConsumerProfile(&domain.SourceDocument{Type: model.Type, Modalities: model.Modalities})
		route, supported := addableRoutes[profile.Operation]
		if !supported || !slices.Contains(model.Endpoints, route.path) {
			continue
		}
		candidates = append(candidates, ref)
		operations[ref.ID] = profile
	}

	documents, _, err := domain.LoadCatalogDocuments(ctx, u.deps, u.store, u.documents, client, source.ID, candidates)
	if err != nil {
		return channeldomain.UpstreamModels{}, err
	}
	for _, ref := range candidates {
		profile := operations[ref.ID]
		item := domain.Pricing{ModelName: ref.ID, UpstreamCatalogRoutesByGroup: map[string][]domain.UpstreamCatalogRoute{"all": {{
			Operation: profile.Operation, Mode: "sync", Endpoint: addableRoutes[profile.Operation].endpoint,
			ModelID: ref.ID, DocumentHash: ref.Hash, Document: documents[ref.ID],
		}}}}
		// A price is confirmed independently of the tariff, so any valid
		// group ratio confirms the same offers.
		minima := domain.CatalogMinimumOperations(u.deps, u.projection.IterateUpstreamProviderOffers(item, "", 1, true, domain.PricingUserContext{}))
		if slices.ContainsFunc(minima, func(operation domain.ConsumerPricingOperation) bool {
			return domain.ConfirmedConsumerPrices(operation.Prices, operation.Tiers)
		}) {
			result.Addable = append(result.Addable, ref.ID)
		}
	}
	u.cache.Store(manifest.Revision, result)
	return result, nil
}
