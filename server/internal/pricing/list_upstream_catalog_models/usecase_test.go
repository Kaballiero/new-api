package list_upstream_catalog_models

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/constant"
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

var errNoHead = errors.New("no published head")

// The fake store publishes no head, and no channel requests a model.
func (fakeStore) Head(context.Context, string) (domain.CatalogSource, error) {
	return domain.CatalogSource{}, errNoHead
}
func (fakeStore) IsMissing(err error) bool { return errors.Is(err, errNoHead) }
func (fakeStore) ReadSelection(context.Context, string, string) (domain.CatalogSelection, error) {
	return domain.CatalogSelection{}, errNoHead
}
func (fakeStore) Abilities() ([]domain.BuildAbility, error) { return nil, nil }
func (fakeStore) BuildChannel(int) (*domain.BuildChannel, error) {
	return nil, errors.New("unexpected channel read")
}

// headStore publishes a head selection of the head models, and one enabled
// OpenRouter channel with the model mapping requests the requested models.
type headStore struct {
	fakeStore
	head, requested                                 []string
	headErr, selectionErr, abilitiesErr, channelErr error
	channelDisabled                                 bool
	baseURL, modelMapping                           string
}

func (store headStore) Head(context.Context, string) (domain.CatalogSource, error) {
	if store.headErr != nil {
		return domain.CatalogSource{}, store.headErr
	}
	return domain.CatalogSource{Revision: strings.Repeat("f", 64)}, nil
}
func (store headStore) ReadSelection(context.Context, string, string) (domain.CatalogSelection, error) {
	selection := domain.CatalogSelection{}
	for _, id := range store.head {
		selection.Models = append(selection.Models, domain.CatalogModelReference{ID: id})
	}
	return selection, store.selectionErr
}
func (store headStore) Abilities() ([]domain.BuildAbility, error) {
	abilities := []domain.BuildAbility{}
	for _, model := range store.requested {
		abilities = append(abilities, domain.BuildAbility{Model: model, Group: "default", ChannelId: 1, ChannelType: constant.ChannelTypeOpenRouter})
	}
	return abilities, store.abilitiesErr
}
func (store headStore) BuildChannel(int) (*domain.BuildChannel, error) {
	channel := &domain.BuildChannel{Id: 1, Name: "openrouter", Type: constant.ChannelTypeOpenRouter, Status: constant.ChannelStatusEnabled, BaseURL: &store.baseURL, ModelMapping: &store.modelMapping}
	if store.channelDisabled {
		channel.Status = constant.ChannelStatusManuallyDisabled
	}
	return channel, store.channelErr
}

// testStore is the store and the channels of a test usecase.
type testStore interface {
	Store
	domain.CatalogChannels
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

func newUsecase(store testStore, documents domain.CatalogDocumentCache, cache Cache, client *fakeClient, stored ...catalogModel) Usecase {
	deps := pricingadapter.Capabilities()
	catalog := fakeCatalog{CatalogReader: deps.Catalog, documents: map[string]*domain.SourceDocument{}}
	for _, model := range stored {
		catalog.documents[model.index.ID] = model.document
	}
	deps.Catalog = catalog
	return NewUsecase(deps, domain.NewProjection(domain.ProjectionRuntime{Capabilities: deps}), store, documents, cache, store, func() (domain.CatalogSource, bool, error) {
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

	models, err := newUsecase(store, &pricingadapter.UpstreamCatalogDocumentsCache{}, &pricingadapter.UpstreamCatalogModelsCache{}, client, imported).Execute(context.Background())
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

			models, err := newUsecase(fakeStore{}, &pricingadapter.UpstreamCatalogDocumentsCache{}, &pricingadapter.UpstreamCatalogModelsCache{}, client).Execute(context.Background())
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

	_, err := NewUsecase(deps, projection, fakeStore{}, &pricingadapter.UpstreamCatalogDocumentsCache{}, &pricingadapter.UpstreamCatalogModelsCache{}, fakeStore{}, func() (domain.CatalogSource, bool, error) { return domain.CatalogSource{}, false, nil }, connect).Execute(context.Background())
	require.EqualError(t, err, "upstream catalog source is not configured")

	client := newClient(strings.Repeat("1", 64), textModel("acme/chat", []string{chatPath}, nil))
	client.index.Models[0].Hash = strings.Repeat("2", 64)
	_, err = newUsecase(fakeStore{}, &pricingadapter.UpstreamCatalogDocumentsCache{}, &pricingadapter.UpstreamCatalogModelsCache{}, client).Execute(context.Background())
	require.EqualError(t, err, "upstream catalog index does not match manifest")
}

func TestExecuteReusesModelsOfSameRevision(t *testing.T) {
	model := textModel("acme/chat", []string{chatPath}, &domain.SourceEligibility{Eligible: true})
	cache := &pricingadapter.UpstreamCatalogModelsCache{}
	first := newClient(strings.Repeat("1", 64), model)
	firstModels, err := newUsecase(fakeStore{}, &pricingadapter.UpstreamCatalogDocumentsCache{}, cache, first).Execute(context.Background())
	require.NoError(t, err)

	second := newClient(strings.Repeat("1", 64), model)
	secondModels, err := newUsecase(fakeStore{}, &pricingadapter.UpstreamCatalogDocumentsCache{}, cache, second).Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, firstModels, secondModels)
	assert.Equal(t, channeldomain.UpstreamModels{Listed: []string{"acme/chat"}, Addable: []string{"acme/chat"}, Shortcuts: []string{":nitro", ":floor"}}, secondModels)
	assert.Empty(t, second.downloaded)

	republished := newClient(strings.Repeat("2", 64), model)
	_, err = newUsecase(fakeStore{}, &pricingadapter.UpstreamCatalogDocumentsCache{}, cache, republished).Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"acme/chat"}, republished.downloaded)
}

func TestExecuteReusesDecodedStoredDocuments(t *testing.T) {
	eligible := &domain.SourceEligibility{Eligible: true}
	imported := textModel("acme/imported", []string{chatPath}, eligible)
	fresh := textModel("acme/chat", []string{chatPath}, eligible)
	documents := &pricingadapter.UpstreamCatalogDocumentsCache{}
	// The second revision reads no stored document: a document decoded from
	// the database before is shared, a downloaded one is not kept.
	for _, tc := range []struct {
		revision string
		store    fakeStore
	}{
		{revision: "1", store: fakeStore{imported.index.Hash: {Hash: imported.index.Hash, ModelID: imported.index.ID, Payload: "stored"}}},
		{revision: "2", store: fakeStore{}},
	} {
		client := newClient(strings.Repeat(tc.revision, 64), fresh, imported)
		delete(client.documents, imported.index.ID)

		models, err := newUsecase(tc.store, documents, &pricingadapter.UpstreamCatalogModelsCache{}, client, imported).Execute(context.Background())
		require.NoError(t, err)
		assert.Equal(t, []string{"acme/chat", "acme/imported"}, models.Addable)
		assert.Equal(t, []string{"acme/chat"}, client.downloaded)
	}
}

// A manifest that lost more than 30% of the published models (the head models
// the channels request) blocks the auto-apply of its models; they are still
// listed and addable. Alias targets and models no channel requests, also of a
// channel with another base URL, are not compared. An enabled channel whose
// model mapping does not resolve leaves the published models unknown: it
// blocks the auto-apply, also without a published head, and is named. The
// flag follows the current head and channels, also for the models of a
// cached revision, and an unreadable head or channel is an error, not an
// unblocked catalog.
func TestExecuteBlocksAutoApplyAfterPublishedModelLoss(t *testing.T) {
	eligible := &domain.SourceEligibility{Eligible: true}
	var listed []catalogModel
	var listedIDs []string
	for _, id := range []string{"acme/m4", "acme/m5", "acme/m6", "acme/m7", "acme/m8", "acme/m9", "acme/m10"} {
		listed = append(listed, textModel(id, []string{chatPath}, eligible))
		listedIDs = append(listedIDs, id)
	}
	lostThree := append([]string{"acme/m1", "acme/m2", "acme/m3"}, listedIDs...)
	lostFour := append([]string{"acme/m0"}, lostThree[:len(lostThree)-1]...)
	// Ten unrequested alias targets the manifest still lists would make the
	// loss of four requested models 4/20 of the whole head.
	targets := []string{"acme/t0", "acme/t1", "acme/t2", "acme/t3", "acme/t4", "acme/t5", "acme/t6", "acme/t7", "acme/t8", "acme/t9"}
	for _, id := range targets {
		listed = append(listed, textModel(id, []string{chatPath}, eligible))
	}
	listedIDs = append(listedIDs, targets...)
	readFailure := errors.New("catalog head unreadable")
	unresolved := []channeldomain.UnresolvedMappingChannel{{ChannelID: 1, ChannelName: "openrouter"}}
	cachedRevision := &pricingadapter.UpstreamCatalogModelsCache{}
	for _, tc := range []struct {
		name       string
		store      testStore
		blocked    bool
		unresolved []channeldomain.UnresolvedMappingChannel
		err        error
	}{
		{name: "no published head", store: fakeStore{}},
		{name: "lost 3 of 10 published models", store: headStore{head: lostThree, requested: lostThree}},
		{name: "lost 4 of 10 published models", store: headStore{head: lostFour, requested: lostFour}, blocked: true},
		{name: "lost 4 of 10 requested models beside 10 alias targets", store: headStore{head: append(slices.Clone(lostFour), targets...), requested: lostFour}, blocked: true},
		{name: "lost models no channel requests", store: headStore{head: lostFour, requested: lostFour[4:]}},
		{name: "lost models of a disabled channel", store: headStore{head: lostFour, requested: lostFour, channelDisabled: true}},
		{name: "lost models of a channel with another base URL", store: headStore{head: lostFour, requested: lostFour, baseURL: "https://proxy.example/api"}},
		{name: "mapping with invalid JSON", store: headStore{head: listedIDs, requested: listedIDs, modelMapping: `{"acme/m4":`}, blocked: true, unresolved: unresolved},
		{name: "mapping with a cycle", store: headStore{head: listedIDs, requested: listedIDs, modelMapping: `{"acme/m4":"acme/m5","acme/m5":"acme/m4"}`}, blocked: true, unresolved: unresolved},
		{name: "mapping with a cycle without a published head", store: headStore{requested: listedIDs, modelMapping: `{"acme/m4":"acme/m5","acme/m5":"acme/m4"}`, headErr: errNoHead}, blocked: true, unresolved: unresolved},
		{name: "unresolved mapping of a channel with another base URL", store: headStore{head: listedIDs, requested: listedIDs, modelMapping: `{"acme/m4":`, baseURL: "https://proxy.example/api"}},
		{name: "unresolved mapping of a disabled channel", store: headStore{head: listedIDs, requested: listedIDs, modelMapping: `{"acme/m4":`, channelDisabled: true}},
		{name: "head without the lost models", store: headStore{head: listedIDs, requested: listedIDs}},
		{name: "head unreadable", store: headStore{head: lostFour, requested: lostFour, headErr: readFailure}, err: readFailure},
		{name: "head selection unreadable", store: headStore{head: lostFour, requested: lostFour, selectionErr: readFailure}, err: readFailure},
		{name: "abilities unreadable", store: headStore{head: listedIDs, requested: listedIDs, abilitiesErr: readFailure}, err: readFailure},
		{name: "channel unreadable", store: headStore{head: listedIDs, requested: listedIDs, channelErr: readFailure}, err: readFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, cache := range []Cache{&pricingadapter.UpstreamCatalogModelsCache{}, cachedRevision} {
				models, err := newUsecase(tc.store, &pricingadapter.UpstreamCatalogDocumentsCache{}, cache, newClient(strings.Repeat("1", 64), listed...)).Execute(context.Background())
				if tc.err != nil {
					require.ErrorIs(t, err, tc.err)
					continue
				}
				require.NoError(t, err)
				assert.Equal(t, tc.blocked, models.AutoApplyBlocked)
				assert.Equal(t, tc.unresolved, models.UnresolvedMappingChannels)
				assert.Equal(t, listedIDs, models.Listed)
				assert.Equal(t, listedIDs, models.Addable)
			}
		})
	}
}
