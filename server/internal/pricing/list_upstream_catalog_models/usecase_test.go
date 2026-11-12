package list_upstream_catalog_models

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const chatPath = "/api/v1/chat/completions"

type fakeStore map[string]domain.CatalogDocument

func (store fakeStore) Documents(_ context.Context, _ string, hashes []string) (map[string]domain.CatalogDocument, error) {
	result := map[string]domain.CatalogDocument{}
	for _, hash := range hashes {
		if row, exists := store[hash]; exists {
			result[hash] = row
		}
	}
	return result, nil
}

// fakeCatalog decodes a stored document by its model id.
type fakeCatalog struct {
	domain.CatalogReader
	documents map[string]*domain.SourceDocument
}

func (catalog fakeCatalog) DecodeDocument(_ []byte, ref domain.SourceReference) (*domain.SourceDocument, error) {
	return catalog.documents[ref.ID], nil
}

type fakeClient struct {
	manifests     []domain.SourceManifest
	index         domain.SourceIndex
	documents     map[string]*domain.SourceDocument
	manifestReads int
	mutex         sync.Mutex
	downloaded    []string
}

func (client *fakeClient) Manifest(context.Context) (domain.SourceManifest, error) {
	manifest := client.manifests[min(client.manifestReads, len(client.manifests)-1)]
	client.manifestReads++
	return manifest, nil
}
func (client *fakeClient) Index(context.Context) (domain.SourceIndex, error) {
	return client.index, nil
}
func (client *fakeClient) Document(_ context.Context, ref domain.SourceReference) ([]byte, *domain.SourceDocument, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.downloaded = append(client.downloaded, ref.ID)
	return nil, client.documents[ref.ID], nil
}

// catalogModel is one model of a test publication: its index entry and the
// document the index projects.
type catalogModel struct {
	index    domain.SourceIndexModel
	document *domain.SourceDocument
}

func textModel(id string, endpoints []string, eligibility *domain.SourceEligibility) catalogModel {
	hash := domain.CatalogHash([]byte(id))
	modalities := domain.SourceModalities{Input: []string{"text"}, Output: []string{"text"}}
	amount := "0.8"
	rate := domain.SourceRate{Parameter: "prompt", ComponentID: "input", AmountUSD: &amount, Unit: "million_tokens", Quantity: "1", Variant: json.RawMessage(`{}`), Conditions: json.RawMessage(`{}`), Provenance: json.RawMessage(`{"source":"generic"}`)}
	rate.Normalization.Comparable = true
	rate.Normalization.Effective = true
	document := &domain.SourceDocument{ID: id, Type: "text", Modalities: modalities, Offers: []domain.SourceOffer{{
		ID: id + "/endpoint", Scope: "endpoint", Variant: json.RawMessage(`{"variant":"standard"}`), Eligibility: eligibility, Components: []domain.SourceRate{rate},
	}}}
	return catalogModel{index: domain.SourceIndexModel{ID: id, Hash: hash, Type: "text", Modalities: modalities, Endpoints: endpoints}, document: document}
}

func newClient(revision string, models ...catalogModel) *fakeClient {
	client := &fakeClient{index: domain.SourceIndex{Revision: revision}, documents: map[string]*domain.SourceDocument{}}
	manifest := domain.SourceManifest{Revision: revision}
	for _, model := range models {
		manifest.Models = append(manifest.Models, domain.SourceReference{ID: model.index.ID, Hash: model.index.Hash})
		client.index.Models = append(client.index.Models, model.index)
		client.documents[model.index.ID] = model.document
	}
	client.manifests = []domain.SourceManifest{manifest}
	return client
}

func newUsecase(store fakeStore, client *fakeClient, stored ...catalogModel) Usecase {
	deps := pricingadapter.Capabilities()
	catalog := fakeCatalog{CatalogReader: deps.Catalog, documents: map[string]*domain.SourceDocument{}}
	for _, model := range stored {
		catalog.documents[model.index.ID] = model.document
	}
	deps.Catalog = catalog
	return NewUsecase(deps, domain.NewProjection(domain.ProjectionRuntime{Capabilities: deps}), store, func() (domain.CatalogSource, bool, error) {
		return domain.CatalogSource{ID: strings.Repeat("a", 64)}, true, nil
	}, func(domain.CatalogSource) (Client, error) { return client, nil })
}

func TestExecuteSelectsAddableModels(t *testing.T) {
	eligible := &domain.SourceEligibility{Eligible: true}
	priced := textModel("acme/chat", []string{chatPath}, eligible)
	imported := textModel("acme/imported", []string{chatPath}, eligible)
	alias := textModel("acme/latest", []string{chatPath}, eligible)
	alias.index.Alias = &domain.SourceAlias{TargetID: "acme/chat"}
	free := textModel("acme/chat:free", []string{chatPath}, eligible)
	batch := textModel("acme/chat:batch", []string{chatPath}, eligible)
	speech := textModel("acme/voice", []string{"/api/v1/audio/speech"}, eligible)
	speech.index.Type = "speech"
	speech.index.Modalities.Output = []string{"speech"}
	withoutChat := textModel("acme/responses-only", []string{"/api/v1/responses"}, eligible)
	unknownEndpoints := textModel("acme/unknown-endpoints", nil, eligible)
	requestDependent := textModel("acme/request-dependent", []string{chatPath}, nil)
	ineligible := textModel("acme/ineligible", []string{chatPath}, &domain.SourceEligibility{Eligible: false})

	client := newClient(strings.Repeat("1", 64), alias, batch, free, priced, imported, ineligible, requestDependent, withoutChat, unknownEndpoints, speech)
	// An imported document is read from the database, not downloaded.
	store := fakeStore{imported.index.Hash: {Hash: imported.index.Hash, ModelID: imported.index.ID, Payload: "stored"}}
	delete(client.documents, imported.index.ID)

	models, err := newUsecase(store, client, imported).Execute(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []string{"acme/latest", "acme/chat:batch", "acme/chat:free", "acme/chat", "acme/imported", "acme/ineligible", "acme/request-dependent", "acme/responses-only", "acme/unknown-endpoints", "acme/voice"}, models.Listed)
	assert.Equal(t, []string{"acme/chat", "acme/imported"}, models.Addable)
	assert.ElementsMatch(t, []string{"acme/chat", "acme/ineligible", "acme/request-dependent"}, client.downloaded)
}

func TestExecuteRequiresOneRevision(t *testing.T) {
	model := textModel("acme/chat", []string{chatPath}, &domain.SourceEligibility{Eligible: true})
	for _, tc := range []struct {
		name              string
		manifestRevisions []string
		err               string
	}{
		{name: "same revision", manifestRevisions: []string{"1"}},
		{name: "publication between reads", manifestRevisions: []string{"0", "1"}},
		{name: "index of another publication", manifestRevisions: []string{"0", "0"}, err: "upstream catalog index revision differs from manifest revision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newClient(strings.Repeat("1", 64), model)
			manifest := client.manifests[0]
			client.manifests = nil
			for _, revision := range tc.manifestRevisions {
				manifest.Revision = strings.Repeat(revision, 64)
				client.manifests = append(client.manifests, manifest)
			}

			models, err := newUsecase(fakeStore{}, client).Execute(context.Background())
			if tc.err != "" {
				require.EqualError(t, err, tc.err)
				assert.Equal(t, 2, client.manifestReads)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []string{"acme/chat"}, models.Addable)
			assert.Len(t, client.manifests, client.manifestReads)
		})
	}
}

func TestExecuteFailsWithoutCatalog(t *testing.T) {
	deps := pricingadapter.Capabilities()
	projection := domain.NewProjection(domain.ProjectionRuntime{Capabilities: deps})
	connect := func(domain.CatalogSource) (Client, error) { return nil, errors.New("unexpected connection") }

	_, err := NewUsecase(deps, projection, fakeStore{}, func() (domain.CatalogSource, bool, error) { return domain.CatalogSource{}, false, nil }, connect).Execute(context.Background())
	require.EqualError(t, err, "upstream catalog source is not configured")

	client := newClient(strings.Repeat("1", 64), textModel("acme/chat", []string{chatPath}, nil))
	client.index.Models[0].Hash = strings.Repeat("2", 64)
	_, err = newUsecase(fakeStore{}, client).Execute(context.Background())
	require.EqualError(t, err, "upstream catalog index does not match manifest")
}

func TestChannelUpstreamModelsListRoutedTargets(t *testing.T) {
	catalogModels := domain.UpstreamCatalogModels{Listed: []string{"acme/chat", "acme/other"}, Addable: []string{"acme/chat"}}
	mapping := `{"fast":"acme/chat:nitro","cheap":"acme/gone:floor"}`
	channel := &domain.BuildChannel{Models: "fast,cheap,acme/other:floor,acme/chat:online", ModelMapping: &mapping}

	models := catalogModels.ChannelUpstreamModels(pricingadapter.Capabilities(), channel)

	assert.Equal(t, channeldomain.UpstreamModels{Listed: []string{"acme/chat", "acme/other", "acme/chat:nitro", "acme/other:floor"}, Addable: []string{"acme/chat"}}, models)
}
