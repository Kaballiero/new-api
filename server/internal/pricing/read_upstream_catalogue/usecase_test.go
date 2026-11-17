package read_upstream_catalogue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore publishes one selection at a time, none for a selection without
// a normalization version, and records the hashes whose payloads were read.
type fakeStore struct {
	selection  domain.CatalogSelection
	fetchedAt  string
	read       [][]string
	selections int
	// onHead runs once at the next head read.
	onHead func()
}

func (store *fakeStore) Head(context.Context, string) (domain.CatalogSource, error) {
	if store.onHead != nil {
		store.onHead()
		store.onHead = nil
	}
	if store.selection.NormalizationVersion == "" {
		return domain.CatalogSource{}, nil
	}
	return domain.CatalogSource{Revision: domain.CatalogHash([]byte(store.selection.NormalizationVersion)), FetchedAt: store.fetchedAt}, nil
}
func (store *fakeStore) IsMissing(error) bool { return false }
func (store *fakeStore) ReadSelection(context.Context, string, string) (domain.CatalogSelection, error) {
	store.selections++
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

// fakeCatalog decodes a stored document into a document of its model id with
// one eligible endpoint offer of provider tag "provider".
type fakeCatalog struct{ domain.CatalogReader }

func (fakeCatalog) DecodeDocument(_ []byte, ref domain.SourceReference) (*domain.SourceDocument, error) {
	tag := "provider"
	return &domain.SourceDocument{ID: ref.ID, Modalities: domain.SourceModalities{Output: []string{"text"}}, Offers: []domain.SourceOffer{{
		Scope: "endpoint", Provider: &domain.SourceProvider{Tag: &tag}, Eligibility: &domain.SourceEligibility{Eligible: true},
		Facts: []byte(`{"pricing":{"prompt":"0.000001","completion":"0.000002"}}`),
	}}}, nil
}

func TestExecuteKeepsDocumentsOfPublishedRevision(t *testing.T) {
	first := domain.CatalogModelReference{ID: "acme/first", Hash: domain.CatalogHash([]byte("acme/first"))}
	shared := domain.CatalogModelReference{ID: "acme/shared", Hash: domain.CatalogHash([]byte("acme/shared"))}
	next := domain.CatalogModelReference{ID: "acme/next", Hash: domain.CatalogHash([]byte("acme/next"))}
	deps := pricingadapter.Capabilities()
	deps.Catalog = fakeCatalog{CatalogReader: deps.Catalog}
	source := domain.CatalogHash([]byte("source"))
	otherSource := domain.CatalogHash([]byte("other source"))
	store := &fakeStore{}
	documents := &pricingadapter.UpstreamCatalogDocumentsCache{}
	// Documents are stored per source: another source's document of the same
	// hash is not taken as stored.
	documents.Store(otherSource, first.Hash, &domain.SourceDocument{ID: first.ID})
	usecase := NewUsecase(deps, store, documents, &atomic.Pointer[domain.UpstreamCatalogTariffs]{}, func() (domain.CatalogSource, bool, error) {
		return domain.CatalogSource{ID: source}, true, nil
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
	_, cached := documents.Load(source, first.Hash)
	assert.False(t, cached)
	_, cached = documents.Load(otherSource, first.Hash)
	assert.False(t, cached)
	_, cached = documents.Load(source, next.Hash)
	assert.True(t, cached)
}

// The tariffs of the published revision are published with the head
// fetchedAt; a head that moved only its fetchedAt keeps the tariffs of the
// revision, and a new revision replaces them without changing the previous
// ones a reader may hold.
func TestExecutePublishesTariffsOfPublishedRevision(t *testing.T) {
	model := domain.CatalogModelReference{ID: "acme/model", Hash: domain.CatalogHash([]byte("acme/model"))}
	next := domain.CatalogModelReference{ID: "acme/next", Hash: domain.CatalogHash([]byte("acme/next"))}
	deps := pricingadapter.Capabilities()
	deps.Catalog = fakeCatalog{CatalogReader: deps.Catalog}
	store := &fakeStore{selection: domain.CatalogSelection{NormalizationVersion: "1", Models: []domain.CatalogModelReference{model}}, fetchedAt: "2026-11-16T10:00:00.5Z"}
	tariffs := &atomic.Pointer[domain.UpstreamCatalogTariffs]{}
	usecase := NewUsecase(deps, store, &pricingadapter.UpstreamCatalogDocumentsCache{}, tariffs, func() (domain.CatalogSource, bool, error) {
		return domain.CatalogSource{ID: domain.CatalogHash([]byte("source"))}, true, nil
	})

	_, _, _, err := usecase.Execute(context.Background())
	require.NoError(t, err)
	first := tariffs.Load()
	require.NotNil(t, first)
	assert.Equal(t, time.Date(2026, 11, 16, 10, 0, 0, 5e8, time.UTC), first.FetchedAt)
	require.Contains(t, first.Models, model.ID)
	assert.Equal(t, "provider", first.Models[model.ID].Endpoints[0].Tag)

	store.fetchedAt = "2026-11-17T10:00:00Z"
	_, _, _, err = usecase.Execute(context.Background())
	require.NoError(t, err)
	touched := tariffs.Load()
	assert.Equal(t, time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC), touched.FetchedAt)
	assert.Equal(t, first.Revision, touched.Revision)
	assert.Equal(t, first.Models[model.ID], touched.Models[model.ID])

	store.selection = domain.CatalogSelection{NormalizationVersion: "2", Models: []domain.CatalogModelReference{next}}
	_, _, _, err = usecase.Execute(context.Background())
	require.NoError(t, err)
	assert.Contains(t, tariffs.Load().Models, next.ID)
	assert.NotContains(t, tariffs.Load().Models, model.ID)
	assert.Contains(t, first.Models, model.ID)
	assert.NotContains(t, first.Models, next.ID)

	store.fetchedAt = "invalid"
	_, _, _, err = usecase.Execute(context.Background())
	require.NoError(t, err)
	assert.True(t, tariffs.Load().FetchedAt.IsZero())

	store.selection = domain.CatalogSelection{}
	_, _, _, err = usecase.Execute(context.Background())
	require.NoError(t, err)
	assert.Nil(t, tariffs.Load())
}

// A head that moved only its fetchedAt is read alone: the selection and the
// documents are not read again. A publication made while the head was read
// is not replaced.
func TestExecuteReadsOnlyHeadOfPublishedRevision(t *testing.T) {
	model := domain.CatalogModelReference{ID: "acme/model", Hash: domain.CatalogHash([]byte("acme/model"))}
	next := domain.CatalogModelReference{ID: "acme/next", Hash: domain.CatalogHash([]byte("acme/next"))}
	deps := pricingadapter.Capabilities()
	deps.Catalog = fakeCatalog{CatalogReader: deps.Catalog}
	store := &fakeStore{selection: domain.CatalogSelection{NormalizationVersion: "1", Models: []domain.CatalogModelReference{model}}, fetchedAt: "2026-11-16T10:00:00Z"}
	tariffs := &atomic.Pointer[domain.UpstreamCatalogTariffs]{}
	usecase := NewUsecase(deps, store, &pricingadapter.UpstreamCatalogDocumentsCache{}, tariffs, func() (domain.CatalogSource, bool, error) {
		return domain.CatalogSource{ID: domain.CatalogHash([]byte("source"))}, true, nil
	})
	first, _, _, err := usecase.Execute(context.Background())
	require.NoError(t, err)

	store.fetchedAt = "2026-11-17T10:00:00Z"
	again, hashes, _, err := usecase.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, store.selections)
	assert.Len(t, store.read, 1)
	assert.Same(t, first[model.ID], again[model.ID])
	assert.Equal(t, map[string]string{model.ID: model.Hash}, hashes)
	assert.Equal(t, time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC), tariffs.Load().FetchedAt)

	newer := &domain.UpstreamCatalogTariffs{Revision: "newer"}
	store.selection = domain.CatalogSelection{NormalizationVersion: "2", Models: []domain.CatalogModelReference{next}}
	store.onHead = func() { tariffs.Store(newer) }
	read, _, _, err := usecase.Execute(context.Background())
	require.NoError(t, err)
	assert.Contains(t, read, next.ID)
	assert.Same(t, newer, tariffs.Load())
}
