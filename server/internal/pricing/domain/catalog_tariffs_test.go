package domain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// catalogV13Documents are real catalog documents of normalization version 13
// trimmed to the fields the tariffs read, by model id.
func catalogV13Documents(t *testing.T, deps Capabilities) map[string]*SourceDocument {
	paths, err := filepath.Glob("testdata/catalog_v13/*.json")
	require.NoError(t, err)
	documents := map[string]*SourceDocument{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var document SourceDocument
		require.NoError(t, deps.Codec.Unmarshal(raw, &document))
		documents[document.ID] = &document
	}
	require.Len(t, documents, 4)
	return documents
}

func catalogTestCapabilities() Capabilities {
	return Capabilities{Codec: JSONCodec{Unmarshal: common.Unmarshal}}
}

// The tariffs of a document are its eligible tagged endpoint offers with the
// values the live OpenRouter endpoints of the same tags published at the
// same time; a null limit is no limit.
func TestCatalogEndpointTariffsMapEligibleEndpointOffers(t *testing.T) {
	deps := catalogTestCapabilities()
	tariffs := CatalogEndpointTariffs(deps, catalogV13Documents(t, deps))
	pricing := func(raw string) map[string]json.RawMessage {
		var prices map[string]json.RawMessage
		require.NoError(t, common.UnmarshalJsonStr(raw, &prices))
		return prices
	}
	for _, tc := range []struct {
		model      string
		modalities []string
		endpoints  []NativePricedEndpoint
	}{
		{"cohere/embed-5-fast", []string{"embeddings"}, []NativePricedEndpoint{
			{ModelID: "cohere/embed-5-fast", Tag: "cohere", Quantization: "unknown", ContextLength: 128000, SupportedParameters: []string{},
				Pricing: pricing(`{"completion":"0","discount":0,"image_token":"0.0000004","prompt":"0.00000008"}`)},
		}},
		{"anthropic/claude-sonnet-5.5:batch", []string{"text"}, []NativePricedEndpoint{
			{ModelID: "anthropic/claude-sonnet-5.5:batch", Tag: "anthropic", Quantization: "unknown", ContextLength: 1000000, MaxCompletionTokens: 128000,
				SupportedParameters: []string{"include_reasoning", "max_tokens", "reasoning", "response_format", "stop", "structured_outputs", "tool_choice", "tools", "verbosity", "reasoning_effort"},
				Pricing:             pricing(`{"completion":"0.000005","discount":0,"input_cache_read":"0.00000005","input_cache_write":"0.00000125","input_cache_write_1h":"0.000002","prompt":"0.000001","web_search":"0.01"}`)},
		}},
	} {
		t.Run(tc.model, func(t *testing.T) {
			require.Contains(t, tariffs, tc.model)
			assert.Equal(t, NativeEndpointResponse{ID: tc.model, Endpoints: tc.endpoints, OutputModalities: tc.modalities}, tariffs[tc.model])
		})
	}
	t.Run("ineligible endpoint and model offer price nothing", func(t *testing.T) {
		var tags []string
		for _, endpoint := range tariffs["meta-llama/llama-4-scout"].Endpoints {
			tags = append(tags, endpoint.Tag)
		}
		assert.Equal(t, []string{"deepinfra/fp8", "deepinfra/fp8/flex", "deepinfra/fp8/priority"}, tags)
		assert.Equal(t, "fp8", tariffs["meta-llama/llama-4-scout"].Endpoints[1].Quantization)
		assert.Zero(t, tariffs["meta-llama/llama-4-scout"].Endpoints[1].MaxCompletionTokens)
	})
	t.Run("an endpoint with reasoning supports reasoning_effort as live", func(t *testing.T) {
		for _, endpoint := range tariffs["openai/gpt-5.4"].Endpoints {
			assert.Contains(t, endpoint.SupportedParameters, "reasoning_effort", endpoint.Tag)
		}
		for _, endpoint := range tariffs["meta-llama/llama-4-scout"].Endpoints {
			assert.NotContains(t, endpoint.SupportedParameters, "reasoning_effort", endpoint.Tag)
		}
	})
	t.Run("an eligible endpoint the document does not describe whole leaves the model to live", func(t *testing.T) {
		for name, damage := range map[string]func(*SourceOffer){
			"no tag":                   func(offer *SourceOffer) { offer.Provider = &SourceProvider{} },
			"no pricing":               func(offer *SourceOffer) { offer.Facts = json.RawMessage(`{"pricing":null}`) },
			"undecodable capabilities": func(offer *SourceOffer) { offer.Capabilities = json.RawMessage(`{"context_length":"long"}`) },
			"undecodable variant":      func(offer *SourceOffer) { offer.Variant = json.RawMessage(`[]`) },
		} {
			document := *catalogV13Documents(t, deps)["meta-llama/llama-4-scout"]
			document.Offers = slices.Clone(document.Offers)
			damage(&document.Offers[2])
			assert.Empty(t, CatalogEndpointTariffs(deps, map[string]*SourceDocument{document.ID: &document}), name)
		}
	})
	t.Run("document without an eligible endpoint offer is absent", func(t *testing.T) {
		alias := &SourceDocument{ID: "acme/alias", Alias: &SourceAlias{TargetID: "acme/target"}, Offers: []SourceOffer{}}
		assert.Empty(t, CatalogEndpointTariffs(deps, map[string]*SourceDocument{alias.ID: alias}))
	})
}

// The service tier the gateway reads from an endpoint tag, as for the live
// endpoints, is the service tier the catalog variant of the offer declares.
func TestCatalogEndpointTagServiceTierMatchesVariant(t *testing.T) {
	deps := catalogTestCapabilities()
	documents := catalogV13Documents(t, deps)
	tariffs := CatalogEndpointTariffs(deps, documents)
	compared := map[ServiceTier]int{}
	for id, document := range documents {
		declared := map[string]string{}
		for _, offer := range document.Offers {
			if offer.Provider != nil && offer.Provider.Tag != nil {
				declared[*offer.Provider.Tag] = CatalogServiceTier(deps, offer.Variant)
			}
		}
		for _, endpoint := range tariffs[id].Endpoints {
			tier := EndpointServiceTier(endpoint.Tag)
			assert.Equal(t, declared[endpoint.Tag], string(tier), endpoint.Tag)
			compared[tier]++
		}
	}
	assert.Equal(t, map[ServiceTier]int{ServiceTierDefault: 4, ServiceTierFlex: 2, ServiceTierPriority: 2}, compared)
}

func TestUpstreamTariffModel(t *testing.T) {
	for _, tc := range []struct {
		model string
		batch bool
		want  string
	}{
		{"openai/gpt-5.4", false, "openai/gpt-5.4"},
		{"openai/gpt-5.4:nitro", false, "openai/gpt-5.4"},
		{"openai/gpt-5.4:floor", false, "openai/gpt-5.4"},
		{"openai/gpt-5.4:free", false, "openai/gpt-5.4:free"},
		{"anthropic/claude-sonnet-5.5", true, "anthropic/claude-sonnet-5.5:batch"},
		{"anthropic/claude-sonnet-5.5:batch", true, "anthropic/claude-sonnet-5.5:batch"},
		{"anthropic/claude-sonnet-5.5:nitro", true, "anthropic/claude-sonnet-5.5:batch"},
	} {
		assert.Equal(t, tc.want, UpstreamTariffModel(tc.model, tc.batch), tc.model)
	}
}

// liveTariffStub discovers one tariff or fails, counting its calls.
type liveTariffStub struct {
	tariff string
	err    error
	calls  int
}

func (live *liveTariffStub) Discover(string) (string, error) {
	live.calls++
	return live.tariff, live.err
}

// A model takes its fresh document without asking live, then the live tariff,
// then its stale document beside the live failure; without a document the
// live failure is returned.
func TestModelTariffSourceOrder(t *testing.T) {
	liveDown := errors.New("live down")
	for _, tc := range []struct {
		name       string
		catalog    map[string]string
		fresh      bool
		liveErr    error
		want       string
		wantSource ReserveSource
		wantErr    error
		wantCalls  int
	}{
		{"fresh document", map[string]string{"m": "document"}, true, nil, "document", ReserveSourceCatalog, nil, 0},
		{"stale document and live", map[string]string{"m": "document"}, false, nil, "live", ReserveSourceLive, nil, 1},
		{"no document and live", nil, true, nil, "live", ReserveSourceLive, nil, 1},
		{"stale document and live down", map[string]string{"m": "document"}, false, liveDown, "document", ReserveSourceCatalogStale, liveDown, 1},
		{"no document and live down", map[string]string{"other": "document"}, true, liveDown, "", "", liveDown, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := &liveTariffStub{tariff: "live", err: tc.liveErr}
			tariff, source, err := ModelTariff("m", tc.catalog, tc.fresh, live)
			assert.Equal(t, tc.want, tariff)
			assert.Equal(t, tc.wantSource, source)
			assert.Equal(t, tc.wantErr, err)
			assert.Equal(t, tc.wantCalls, live.calls)
		})
	}
}
