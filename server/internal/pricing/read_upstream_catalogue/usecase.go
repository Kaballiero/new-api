package read_upstream_catalogue

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
)

type Store interface {
	Head(context.Context, string) (domain.CatalogSource, error)
	IsMissing(error) bool
	ReadSelection(context.Context, string, string) (domain.CatalogSelection, error)
	Documents(context.Context, string, []string) (map[string]domain.CatalogDocument, error)
}
type Usecase struct {
	store  Store
	source func() (domain.CatalogSource, bool, error)
}

func NewUsecase(store Store, source func() (domain.CatalogSource, bool, error)) Usecase {
	return Usecase{store: store, source: source}
}
func (u Usecase) Execute(ctx context.Context) (map[string]*catalogsync.Document, map[string]string, bool, error) {
	source, enabled, err := u.source()
	if err != nil || !enabled {
		return nil, nil, enabled, err
	}
	head, err := u.store.Head(ctx, source.ID)
	if err != nil {
		if u.store.IsMissing(err) {
			return map[string]*catalogsync.Document{}, map[string]string{}, true, nil
		}
		return nil, nil, true, err
	}

	if head.Revision == "" {
		return map[string]*catalogsync.Document{}, map[string]string{}, true, nil
	}
	selection, err := u.store.ReadSelection(ctx, source.ID, head.Revision)
	if err != nil {
		return nil, nil, true, err
	}
	hashes := make([]string, 0, len(selection.Models))
	for _, ref := range selection.Models {
		hashes = append(hashes, ref.Hash)
	}
	stored, err := u.store.Documents(ctx, source.ID, hashes)
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
