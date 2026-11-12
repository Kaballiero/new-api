package list_upstream_catalog_models

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"golang.org/x/sync/errgroup"
)

type Store interface {
	Documents(context.Context, string, []string) (map[string]domain.CatalogDocument, error)
}
type Client interface {
	Manifest(context.Context) (domain.SourceManifest, error)
	Index(context.Context) (domain.SourceIndex, error)
	Document(context.Context, domain.SourceReference) ([]byte, *domain.SourceDocument, error)
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
	store      Store
	source     func() (domain.CatalogSource, bool, error)
	connect    func(domain.CatalogSource) (Client, error)
}

func NewUsecase(deps domain.Capabilities, projection *domain.Projection, store Store, source func() (domain.CatalogSource, bool, error), connect func(domain.CatalogSource) (Client, error)) Usecase {
	return Usecase{deps: deps, projection: projection, store: store, source: source, connect: connect}
}

// Execute lists the models of the published upstream catalog. A model is
// addable when it is not an alias, free or batch model, serves an operation
// the gateway supports by its type and declared request path, and has a
// confirmed price of a synchronous offer.
func (u Usecase) Execute(ctx context.Context) (domain.UpstreamCatalogModels, error) {
	source, enabled, err := u.source()
	if err != nil {
		return domain.UpstreamCatalogModels{}, err
	}
	if !enabled {
		return domain.UpstreamCatalogModels{}, errors.New("upstream catalog source is not configured")
	}
	client, err := u.connect(source)
	if err != nil {
		return domain.UpstreamCatalogModels{}, err
	}
	// The manifest and the index are read by two requests; a publication
	// between them changes the revision, so the pair is read once more.
	var manifest domain.SourceManifest
	var index domain.SourceIndex
	for attempt := range 2 {
		if manifest, err = client.Manifest(ctx); err != nil {
			return domain.UpstreamCatalogModels{}, err
		}
		if index, err = client.Index(ctx); err != nil {
			return domain.UpstreamCatalogModels{}, err
		}
		if manifest.Revision == index.Revision {
			break
		}
		if attempt == 1 {
			return domain.UpstreamCatalogModels{}, errors.New("upstream catalog index revision differs from manifest revision")
		}
	}

	indexModels := make(map[string]domain.SourceIndexModel, len(index.Models))
	for _, model := range index.Models {
		indexModels[model.ID] = model
	}
	result := domain.UpstreamCatalogModels{Listed: make([]string, 0, len(manifest.Models)), Addable: []string{}}
	candidates := []domain.SourceIndexModel{}
	operations := map[string]domain.PricingOperation{}
	for _, ref := range manifest.Models {
		result.Listed = append(result.Listed, ref.ID)
		model, exists := indexModels[ref.ID]
		if !exists || model.Hash != ref.Hash {
			return domain.UpstreamCatalogModels{}, errors.New("upstream catalog index does not match manifest")
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
		candidates = append(candidates, model)
		operations[model.ID] = profile
	}

	hashes := make([]string, len(candidates))
	for i, model := range candidates {
		hashes[i] = model.Hash
	}
	stored, err := u.store.Documents(ctx, source.ID, hashes)
	if err != nil {
		return domain.UpstreamCatalogModels{}, err
	}
	documents := make([]*domain.SourceDocument, len(candidates))
	workers, workerCtx := errgroup.WithContext(ctx)
	workers.SetLimit(5)
	for i, model := range candidates {
		workers.Go(func() error {
			ref := domain.SourceReference{ID: model.ID, Hash: model.Hash}
			var document *domain.SourceDocument
			var err error
			if row, exists := stored[model.Hash]; exists {
				document, err = u.deps.Catalog.DecodeDocument([]byte(row.Payload), ref)
			} else {
				_, document, err = client.Document(workerCtx, ref)
			}
			if err != nil {
				return err
			}
			documents[i] = document
			return nil
		})
	}
	if err := workers.Wait(); err != nil {
		return domain.UpstreamCatalogModels{}, err
	}

	for i, model := range candidates {
		profile := operations[model.ID]
		item := domain.Pricing{ModelName: model.ID, UpstreamCatalogRoutesByGroup: map[string][]domain.UpstreamCatalogRoute{"all": {{
			Operation: profile.Operation, Mode: "sync", Endpoint: addableRoutes[profile.Operation].endpoint,
			ModelID: model.ID, DocumentHash: model.Hash, Document: documents[i],
		}}}}
		// A price is confirmed independently of the tariff, so any valid
		// group ratio confirms the same offers.
		minima := domain.CatalogMinimumOperations(u.deps, u.projection.IterateUpstreamProviderOffers(item, "", 1, true, domain.PricingUserContext{}))
		if slices.ContainsFunc(minima, func(operation domain.ConsumerPricingOperation) bool {
			return domain.ConfirmedConsumerPrices(operation.Prices, operation.Tiers)
		}) {
			result.Addable = append(result.Addable, model.ID)
		}
	}
	return result, nil
}
