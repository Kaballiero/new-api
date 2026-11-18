package list_upstream_catalog_models

import (
	"context"
	"errors"
	"slices"
	"strings"

	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Client interface {
	Manifest(context.Context) (domain.SourceManifest, error)
	Index(context.Context) (domain.SourceIndex, error)
	domain.CatalogDocumentClient
}

// Store reads the stored catalog documents and the selection the head
// publishes.
type Store interface {
	domain.CatalogDocumentReader
	Head(context.Context, string) (domain.CatalogSource, error)
	IsMissing(error) bool
	ReadSelection(context.Context, string, string) (domain.CatalogSelection, error)
}

// Cache keeps the upstream models of the last catalog revision, so a
// revision's documents are read and its prices confirmed once.
type Cache interface {
	Load(revision string) (channeldomain.UpstreamModels, bool)
	Store(revision string, models channeldomain.UpstreamModels)
}

// ReserveChecker checks that the gateway admission prices the reserve of a
// reference request of model for operation from the given endpoint tariffs
// alone.
type ReserveChecker interface {
	Check(tariffs map[string]domain.NativeEndpointResponse, model, operation string) error
}

// addableRoute is the request path the catalog declares for a consumer
// operation and the gateway operation its reserve is checked for; an empty
// reserveOperation is not checked.
type addableRoute struct {
	path, reserveOperation string
}

// addableRoutes are the consumer operations a model added from the catalog
// may serve now. The image reserve stays live: an image model is added by its
// confirmed price alone.
var addableRoutes = map[string]addableRoute{
	"text":             {path: "/api/v1/chat/completions", reserveOperation: "chat/completions"},
	"embeddings":       {path: "/api/v1/embeddings", reserveOperation: "embeddings"},
	"image_generation": {path: "/api/v1/images"},
}

// unaddableSuffixes mark catalog models that are not added to a channel: free
// variants and batch models.
var unaddableSuffixes = []string{":free", ":batch"}

type Usecase struct {
	deps       domain.Capabilities
	projection *domain.Projection
	store      Store
	documents  domain.CatalogDocumentCache
	cache      Cache
	channels   domain.CatalogChannels
	reserves   ReserveChecker
	source     func() (domain.CatalogSource, bool, error)
	connect    func(domain.CatalogSource) (Client, error)
}

func NewUsecase(deps domain.Capabilities, projection *domain.Projection, store Store, documents domain.CatalogDocumentCache, cache Cache, channels domain.CatalogChannels, reserves ReserveChecker, source func() (domain.CatalogSource, bool, error), connect func(domain.CatalogSource) (Client, error)) Usecase {
	return Usecase{deps: deps, projection: projection, store: store, documents: documents, cache: cache, channels: channels, reserves: reserves, source: source, connect: connect}
}

// Execute lists the models of the published upstream catalog as the upstream
// models of an OpenRouter channel. A model is addable when it is not an alias,
// free or batch model, serves an operation the gateway supports by its type
// and declared request path, and has a confirmed price of a synchronous offer.
// An addable text or embeddings model whose reserve the endpoint tariffs of
// the candidate documents cannot price is ReserveRefused with the admission
// error; the gateway refuses such a request without a manual reserve.
// The models are AutoApplyBlocked when the manifest lost more than
// domain.MaxCatalogModelLossPercent of the published models
// (domain.CatalogModelLoss), the same comparison that keeps the head, and
// when a channel whose model mapping does not resolve leaves the published
// models unknown: such channels are the UnresolvedMappingChannels.
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

	head, err := u.store.Head(ctx, source.ID)
	if err != nil && !u.store.IsMissing(err) {
		return channeldomain.UpstreamModels{}, err
	}
	requested, unresolved, err := domain.CatalogRequestedModels(u.deps, u.channels)
	if err != nil {
		return channeldomain.UpstreamModels{}, err
	}
	var unresolvedMappingChannels []channeldomain.UnresolvedMappingChannel
	for _, channel := range unresolved {
		unresolvedMappingChannels = append(unresolvedMappingChannels, channeldomain.UnresolvedMappingChannel{ChannelID: channel.Id, ChannelName: channel.Name})
	}
	autoApplyBlocked := len(unresolved) > 0
	if !autoApplyBlocked && head.Revision != "" {
		selection, err := u.store.ReadSelection(ctx, source.ID, head.Revision)
		if err != nil {
			return channeldomain.UpstreamModels{}, err
		}
		_, _, autoApplyBlocked = domain.CatalogModelLoss(selection, requested, manifest)
	}

	if models, cached := u.cache.Load(manifest.Revision); cached {
		models.AutoApplyBlocked = autoApplyBlocked
		models.UnresolvedMappingChannels = unresolvedMappingChannels
		return models, nil
	}

	indexModels := make(map[string]domain.SourceIndexModel, len(index.Models))
	for _, model := range index.Models {
		indexModels[model.ID] = model
	}
	result := channeldomain.UpstreamModels{Listed: make([]string, 0, len(manifest.Models)), Addable: []string{}, ReserveRefused: map[string]string{}, Shortcuts: domain.OpenRouterShortcuts, AutoApplyBlocked: autoApplyBlocked, UnresolvedMappingChannels: unresolvedMappingChannels}
	candidates := []domain.SourceReference{}
	reserveOperations := make(map[string]string)
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
		reserveOperations[ref.ID] = route.reserveOperation
	}

	documents, _, err := domain.LoadCatalogDocuments(ctx, u.deps, u.store, u.documents, client, source.ID, candidates)
	if err != nil {
		return channeldomain.UpstreamModels{}, err
	}
	tariffs := domain.CatalogEndpointTariffs(u.deps, documents)
	for _, ref := range candidates {
		// A price is confirmed independently of the tariff, so any valid
		// group ratio confirms the same offers.
		minima := u.projection.CatalogModelMinimumPrices(ref, documents[ref.ID], 1, true)
		if !slices.ContainsFunc(minima, func(operation domain.ConsumerPricingOperation) bool {
			return domain.ConfirmedConsumerPrices(operation.Prices, operation.Tiers)
		}) {
			continue
		}
		result.Addable = append(result.Addable, ref.ID)
		operation := reserveOperations[ref.ID]
		if operation == "" {
			continue
		}
		if err := u.reserves.Check(tariffs, ref.ID, operation); err != nil {
			result.ReserveRefused[ref.ID] = err.Error()
		}
	}
	u.cache.Store(manifest.Revision, result)
	return result, nil
}
