package read_upstream_catalogue

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Store interface {
	Head(context.Context, string) (domain.CatalogSource, error)
	IsMissing(error) bool
	ReadSelection(context.Context, string, string) (domain.CatalogSelection, error)
	Documents(context.Context, string, []string) (map[string]domain.CatalogDocument, error)
}

// DocumentCache keeps the decoded documents of the published revision.
type DocumentCache interface {
	domain.CatalogDocumentCache
	// Retain releases all documents except those of the source with these
	// hashes.
	Retain(sourceID string, hashes []string)
}

// Tariffs keeps the endpoint tariffs of the published revision for the
// reserve estimates; a publication replaces them whole.
type Tariffs interface {
	Load() *domain.UpstreamCatalogTariffs
	Store(*domain.UpstreamCatalogTariffs)
	CompareAndSwap(old, new *domain.UpstreamCatalogTariffs) bool
}
type Usecase struct {
	deps      domain.Capabilities
	store     Store
	documents DocumentCache
	tariffs   Tariffs
	source    func() (domain.CatalogSource, bool, error)
}

func NewUsecase(deps domain.Capabilities, store Store, documents DocumentCache, tariffs Tariffs, source func() (domain.CatalogSource, bool, error)) Usecase {
	return Usecase{deps: deps, store: store, documents: documents, tariffs: tariffs, source: source}
}

// Execute reads the documents of the published revision and publishes their
// endpoint tariffs (domain.CatalogEndpointTariffs) with the head fetchedAt. A
// revision is read and mapped once: for a head that moved only its fetchedAt,
// only the head is read and the published documents are returned. The
// published tariffs are loaded before the head and replaced only if no other
// publication happened since, so a slow read never publishes an older head
// over a newer one. Without a published revision there are no tariffs; a
// failed read keeps the published ones.
func (u Usecase) Execute(ctx context.Context) (map[string]*domain.SourceDocument, map[string]string, bool, error) {
	source, enabled, err := u.source()
	if err != nil || !enabled {
		return nil, nil, enabled, err
	}
	published := u.tariffs.Load()
	head, err := u.store.Head(ctx, source.ID)
	if err != nil {
		if u.store.IsMissing(err) {
			u.tariffs.Store(nil)
			return map[string]*domain.SourceDocument{}, map[string]string{}, true, nil
		}
		return nil, nil, true, err
	}

	if head.Revision == "" {
		u.tariffs.Store(nil)
		return map[string]*domain.SourceDocument{}, map[string]string{}, true, nil
	}
	// The head stores a validated fetchedAt; one that does not parse leaves
	// the zero time, so the tariffs are stale.
	fetchedAt, _ := time.Parse(time.RFC3339Nano, head.FetchedAt)
	if published != nil && published.Revision == head.Revision {
		touched := *published
		touched.FetchedAt = fetchedAt
		u.tariffs.CompareAndSwap(published, &touched)
		return published.Documents, published.Hashes, true, nil
	}
	selection, err := u.store.ReadSelection(ctx, source.ID, head.Revision)
	if err != nil {
		return nil, nil, true, err
	}
	refs := make([]domain.SourceReference, len(selection.Models))
	hashes := make([]string, len(selection.Models))
	byID := make(map[string]string, len(selection.Models))
	for i, ref := range selection.Models {
		refs[i] = domain.SourceReference(ref)
		hashes[i] = ref.Hash
		byID[ref.ID] = ref.Hash
	}
	models, missing, err := domain.LoadStoredCatalogDocuments(ctx, u.deps, u.store, u.documents, source.ID, refs)
	if err != nil {
		return nil, nil, true, err
	}
	if len(missing) > 0 {
		return nil, nil, true, errors.New("published catalog document missing")
	}
	u.documents.Retain(source.ID, hashes)
	resolveAlias := u.deps.Catalog.Aliases(models)
	for id := range models {
		if _, err := resolveAlias(id); err != nil {
			return nil, nil, true, err
		}
	}
	u.tariffs.CompareAndSwap(published, &domain.UpstreamCatalogTariffs{
		Revision: head.Revision, FetchedAt: fetchedAt,
		Models: domain.CatalogEndpointTariffs(u.deps, models), Documents: models, Hashes: byID,
	})
	return models, byID, true, nil
}
