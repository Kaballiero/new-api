package read_upstream_catalogue

import (
	"context"
	"errors"

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
	// Retain releases the documents of all other hashes.
	Retain(hashes []string)
}
type Usecase struct {
	deps      domain.Capabilities
	store     Store
	documents DocumentCache
	source    func() (domain.CatalogSource, bool, error)
}

func NewUsecase(deps domain.Capabilities, store Store, documents DocumentCache, source func() (domain.CatalogSource, bool, error)) Usecase {
	return Usecase{deps: deps, store: store, documents: documents, source: source}
}
func (u Usecase) Execute(ctx context.Context) (map[string]*domain.SourceDocument, map[string]string, bool, error) {
	source, enabled, err := u.source()
	if err != nil || !enabled {
		return nil, nil, enabled, err
	}
	head, err := u.store.Head(ctx, source.ID)
	if err != nil {
		if u.store.IsMissing(err) {
			return map[string]*domain.SourceDocument{}, map[string]string{}, true, nil
		}
		return nil, nil, true, err
	}

	if head.Revision == "" {
		return map[string]*domain.SourceDocument{}, map[string]string{}, true, nil
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
	models, unstored, err := domain.LoadStoredCatalogDocuments(ctx, u.deps, u.store, u.documents, source.ID, refs)
	if err != nil {
		return nil, nil, true, err
	}
	if len(unstored) > 0 {
		return nil, nil, true, errors.New("published catalog document missing")
	}
	u.documents.Retain(hashes)
	resolveAlias := u.deps.Catalog.Aliases(models)
	for id := range models {
		if _, err := resolveAlias(id); err != nil {
			return nil, nil, true, err
		}
	}
	return models, byID, true, nil
}
