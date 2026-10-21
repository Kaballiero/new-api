package model

import (
	"context"
	"errors"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/ensure_catalog_source"
	"github.com/QuantumNous/new-api/internal/pricing/publish_catalog_selection"
	"gorm.io/gorm"
	"time"
)

var ErrCatalogSourceChanged = errors.New("catalog source changed before publication")

type CatalogSource = store.CatalogSource
type CatalogRevision = store.CatalogRevision
type CatalogDocument = store.CatalogDocument
type CatalogModelReference = domain.CatalogModelReference
type CatalogSelection = domain.CatalogSelection
type CatalogPublication = domain.CatalogPublication

func CatalogHash(data []byte) string { return domain.CatalogHash(data) }
func NewImmutableCatalogPersistence() store.Immutable {
	return store.NewImmutable(func() *gorm.DB { return DB })
}
func EnsureCatalogSource(ctx context.Context, source CatalogSource) (CatalogSource, error) {
	v, err := ensure_catalog_source.NewUsecase(NewImmutableCatalogPersistence()).Execute(ctx, domain.CatalogSource(source))
	return CatalogSource(v), err
}
func GetCatalogDocuments(ctx context.Context, sourceID string, hashes []string) (map[string]CatalogDocument, error) {
	rows, err := NewImmutableCatalogPersistence().Documents(ctx, sourceID, hashes)
	if err != nil {
		return nil, err
	}
	out := make(map[string]CatalogDocument, len(rows))
	for key, v := range rows {
		out[key] = CatalogDocument(v)
	}
	return out, nil
}
func PublishCatalogSelection(ctx context.Context, expected CatalogSource, selection CatalogSelection, remote, fetched string, documents []CatalogDocument) (CatalogPublication, error) {
	var values []domain.CatalogDocument
	for _, v := range documents {
		values = append(values, domain.CatalogDocument(v))
	}
	return publish_catalog_selection.NewUsecase(NewImmutableCatalogPersistence(), func() error { return ErrCatalogSourceChanged }, time.Now).Execute(ctx, domain.CatalogSource(expected), selection, remote, fetched, values)
}
func ReadCatalogSelection(ctx context.Context, sourceID, revision string) (CatalogSelection, error) {
	return NewImmutableCatalogPersistence().ReadSelection(ctx, sourceID, revision)
}
