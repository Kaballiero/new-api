package read_upstream_catalogue

import (
	"context"
	"testing"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore publishes one selection at a time and records the hashes whose
// payloads were read.
type fakeStore struct {
	selection domain.CatalogSelection
	read      [][]string
}

func (store *fakeStore) Head(context.Context, string) (domain.CatalogSource, error) {
	return domain.CatalogSource{Revision: domain.CatalogHash([]byte(store.selection.NormalizationVersion))}, nil
}
func (store *fakeStore) IsMissing(error) bool { return false }
func (store *fakeStore) ReadSelection(context.Context, string, string) (domain.CatalogSelection, error) {
	return store.selection, nil
}
func (store *fakeStore) Documents(_ context.Context, _ string, hashes []string) (map[string]domain.CatalogDocument, error) {
	store.read = append(store.read, hashes)
	result := make(map[string]domain.CatalogDocument, len(hashes))
	for _, hash := range hashes {
		result[hash] = domain.CatalogDocument{Hash: hash, Payload: "stored"}
	}
	return result, nil
}

// fakeCatalog decodes a stored document into a document of its model id.
type fakeCatalog struct{ domain.CatalogReader }

func (fakeCatalog) DecodeDocument(_ []byte, ref domain.SourceReference) (*domain.SourceDocument, error) {
	return &domain.SourceDocument{ID: ref.ID}, nil
}

func TestExecuteKeepsDocumentsOfPublishedRevision(t *testing.T) {
	first := domain.CatalogModelReference{ID: "acme/first", Hash: domain.CatalogHash([]byte("acme/first"))}
	shared := domain.CatalogModelReference{ID: "acme/shared", Hash: domain.CatalogHash([]byte("acme/shared"))}
	next := domain.CatalogModelReference{ID: "acme/next", Hash: domain.CatalogHash([]byte("acme/next"))}
	deps := pricingadapter.Capabilities()
	deps.Catalog = fakeCatalog{CatalogReader: deps.Catalog}
	store := &fakeStore{}
	documents := &pricingadapter.UpstreamCatalogDocumentsCache{}
	usecase := NewUsecase(deps, store, documents, func() (domain.CatalogSource, bool, error) {
		return domain.CatalogSource{ID: domain.CatalogHash([]byte("source"))}, true, nil
	})

	store.selection = domain.CatalogSelection{NormalizationVersion: "1", Models: []domain.CatalogModelReference{first, shared}}
	previous, _, _, err := usecase.Execute(context.Background())
	require.NoError(t, err)
	store.selection = domain.CatalogSelection{NormalizationVersion: "2", Models: []domain.CatalogModelReference{shared, next}}
	current, hashes, enabled, err := usecase.Execute(context.Background())
	require.NoError(t, err)

	assert.True(t, enabled)
	assert.Equal(t, map[string]string{shared.ID: shared.Hash, next.ID: next.Hash}, hashes)
	// A document of both revisions is decoded once; the next revision reads
	// only its new document.
	assert.Equal(t, [][]string{{first.Hash, shared.Hash}, {next.Hash}}, store.read)
	assert.Same(t, previous[shared.ID], current[shared.ID])
	// A document the published revision no longer references is released.
	_, cached := documents.Load(first.Hash)
	assert.False(t, cached)
	_, cached = documents.Load(next.Hash)
	assert.True(t, cached)
}
