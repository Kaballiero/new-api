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
// trimmed to the fields the tariffs read, by model id: four text and
// embeddings documents, two video documents, five speech documents and one
// transcription document.
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
	require.Len(t, documents, 12)
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

// The unit prices of an endpoint are its model_page components per dimension:
// amountUSD / quantity, the maximum over the components of a dimension. A null
// amount stays unknown, and a component without a billable prices an unknown
// dimension.
func TestCatalogEndpointUnitPrices(t *testing.T) {
	deps := catalogTestCapabilities()
	documents := catalogV13Documents(t, deps)
	// unitPrices spells the unit prices of the first endpoint of a model as
	// billable/unit=USD, an unknown price as "unknown".
	unitPrices := func(document *SourceDocument) []string {
		tariffs := CatalogEndpointTariffs(deps, map[string]*SourceDocument{document.ID: document})
		require.Contains(t, tariffs, document.ID)
		var spelled []string
		for _, price := range tariffs[document.ID].Endpoints[0].UnitPrices {
			usd := "unknown"
			if price.USD != nil {
				usd = price.USD.String()
			}
			spelled = append(spelled, string(price.Dimension.Billable)+"/"+string(price.Dimension.Unit)+"="+usd)
		}
		return spelled
	}
	for _, tc := range []struct {
		name, model string
		want        []string
	}{
		{"per character", "deepgram/aura-2", []string{"output_speech/character=0.00003"}},
		{"per UTF-8 byte", "fish-audio/s1", []string{"output_speech/utf8_byte=0.000015"}},
		{"per second, the display row per minute", "bytedance-seed/seed-audio-1-0", []string{"output_audio/second=0.0025"}},
		{"per million tokens", "google/gemini-3.8-flash-tts", []string{"input_text/million_tokens=0.5", "output_audio/million_tokens=9"}},
		{"a component without a billable is unknown", "elevenlabs/scribe-v2", []string{"input_audio/second=0.0000611111111111", "/=unknown"}},
		// prompt is 0.000015; the native SKU before the endpoint discount is
		// the maximum of the dimension.
		{"the component price, not prompt", "heygen/heygen-voice-1", []string{"output_speech/character=0.00003"}},
		{"a text endpoint has none", "openai/gpt-5.4", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, unitPrices(documents[tc.model]))
		})
	}
	t.Run("a null amount stays unknown", func(t *testing.T) {
		document := *documents["deepgram/aura-2"]
		document.Offers = slices.Clone(document.Offers)
		document.Offers[0].Components = slices.Clone(document.Offers[0].Components)
		document.Offers[0].Components[1].AmountUSD = nil
		assert.Equal(t, []string{"output_speech/character=unknown"}, unitPrices(&document))
	})
	t.Run("a generic component is not a unit price", func(t *testing.T) {
		document := *documents["deepgram/aura-2"]
		document.Offers = slices.Clone(document.Offers)
		document.Offers[0].Components = slices.Clone(document.Offers[0].Components)
		for index := range document.Offers[0].Components {
			document.Offers[0].Components[index].Provenance = json.RawMessage(`{"scope":"endpoint","source":"generic"}`)
		}
		assert.Nil(t, unitPrices(&document))
	})
}

// The video tariffs of a document are the /videos/models entry of its
// eligible endpoint offer as OpenRouter published it; any other shape leaves
// the model to live pricing.
func TestCatalogVideoTariffsMapTheEligibleVideoEntry(t *testing.T) {
	deps := catalogTestCapabilities()
	documents := catalogV13Documents(t, deps)
	assert.Equal(t, map[string]VideoModelPricing{
		"alibaba/wan-2.6": {
			ID: "alibaba/wan-2.6",
			PricingSKUs: map[string]string{
				"image_to_video_duration_seconds_1080p": "0.15", "image_to_video_duration_seconds_720p": "0.10",
				"text_to_video_duration_seconds_1080p": "0.12", "text_to_video_duration_seconds_480p": "0.04", "text_to_video_duration_seconds_720p": "0.08",
			},
			SupportedDurations: []int{10, 5}, SupportedSizes: []string{"1080x1920", "1280x720", "1920x1080", "720x1280"},
			SupportedResolutions: []string{"1080p", "720p"}, SupportedAspectRatios: []string{"16:9", "9:16"},
		},
		"runway/gen-4.5": {
			ID: "runway/gen-4.5", PricingSKUs: map[string]string{"cents_per_second_output": "12"},
			SupportedDurations: []int{10, 2, 3, 4, 5, 6, 7, 8, 9}, SupportedSizes: []string{"1280x720", "720x1280"},
			SupportedResolutions: []string{"720p"}, SupportedAspectRatios: []string{"16:9", "9:16"},
		},
	}, CatalogVideoTariffs(deps, documents))

	for name, damage := range map[string]func(*SourceDocument){
		"ineligible offer": func(document *SourceDocument) {
			document.Offers[0].Eligibility = &SourceEligibility{Eligible: false}
		},
		"two entries": func(document *SourceDocument) {
			document.Offers = append(document.Offers, document.Offers[0])
		},
		"malformed entry": func(document *SourceDocument) {
			document.Offers[0].Facts = json.RawMessage(`{"raw":{"video_api":{"id":"runway/gen-4.5","pricing_skus":{"cents_per_second_output":12}}}}`)
		},
		"entry of another model": func(document *SourceDocument) {
			document.Offers[0].Facts = json.RawMessage(`{"raw":{"video_api":{"id":"runway/gen-4","pricing_skus":{"cents_per_second_output":"12"}}}}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			document := *catalogV13Documents(t, deps)["runway/gen-4.5"]
			document.Offers = slices.Clone(document.Offers)
			damage(&document)
			assert.Empty(t, CatalogVideoTariffs(deps, map[string]*SourceDocument{document.ID: &document}))
		})
	}
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
	assert.Equal(t, map[ServiceTier]int{ServiceTierDefault: 11, ServiceTierFlex: 2, ServiceTierPriority: 2}, compared)
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
