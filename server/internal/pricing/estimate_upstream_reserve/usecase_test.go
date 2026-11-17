package estimate_upstream_reserve

import (
	"encoding/json"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLive is the live endpoints of a gateway by tariff model; a model it does
// not publish fails discovery. Asked records the discovered models.
type fakeLive struct {
	responses map[string]domain.NativeEndpointResponse
	asked     []string
}

func (live *fakeLive) Discover(model string) (domain.NativeEndpointResponse, error) {
	live.asked = append(live.asked, model)
	response, published := live.responses[model]
	if !published {
		return domain.NativeEndpointResponse{}, errors.New("OpenRouter pricing discovery failed")
	}
	return response, nil
}

// liveOnly is the scenario without a published upstream catalog.
func liveOnly(deps domain.Capabilities) Usecase {
	return NewUsecase(deps, &atomic.Pointer[domain.UpstreamCatalogTariffs]{}, time.Now, func() int { return 24 })
}

// textInput is a nonstream text Chat request of 1000 prompt tokens and
// max_tokens 10 to model, routed to the live endpoints, as the gateway passes
// it.
func textInput(model string, endpoints ...domain.NativePricedEndpoint) Input {
	outputLimit := 10
	return Input{
		Model:               model,
		Models:              []string{model},
		Live:                &fakeLive{responses: map[string]domain.NativeEndpointResponse{model: {ID: model, Endpoints: endpoints}}},
		TextOutput:          true,
		PromptTokens:        1000,
		Count:               1,
		OutputLimit:         &outputLimit,
		DefaultOutputTokens: 8192,
		MaxTokensLimit:      math.MaxInt32 / 2,
	}
}

func endpointPricing(t *testing.T, deps domain.Capabilities, raw string) map[string]json.RawMessage {
	var pricing map[string]json.RawMessage
	require.NoError(t, deps.Codec.Unmarshal([]byte(raw), &pricing))
	return pricing
}

func TestEndpointFlexFallbackPriceBounds(t *testing.T) {
	deps := pricingadapter.Capabilities()
	firstStatus, secondStatus, localUSD := -2, -5, .25
	for _, tc := range []struct {
		name, fallbackPricing, flexPricing, secondFlexPricing string
		local, fail                                           bool
		want                                                  float64
	}{
		{"both prices must fit the same flex tariff", `{"prompt":"0.002","completion":"0.002","input_cache_write":"0.006"}`, `{"prompt":"0.001","completion":"0.003"}`, `{"prompt":"0.003","completion":"0.001"}`, false, false, 3.01},
		{"input price exceeds flex bound", `{"prompt":"0.004","completion":"0.001","input_cache_write":"0.006"}`, `{"prompt":"0.003","completion":"0.004"}`, "", false, false, 3.04},
		{"output price exceeds flex bound", `{"prompt":"0.001","completion":"0.005","input_cache_write":"0.006"}`, `{"prompt":"0.003","completion":"0.004"}`, "", false, false, 3.04},
		{"equal prices retain fallback cache maximum", `{"prompt":"0.003","completion":"0.004","input_cache_write":"0.006"}`, `{"prompt":"0.003","completion":"0.004"}`, "", false, false, 6.04},
		{"unknown flex price needs explicit cover", `{"prompt":"0.001","completion":"0.002","input_cache_write":"0.005"}`, `null`, "", false, true, 0},
		{"unknown flex price preserves known fallback floor", `{"prompt":"0.001","completion":"0.002","input_cache_write":"0.005"}`, `null`, "", true, false, 5.02},
		{"unknown fallback price remains strict", `null`, `{"prompt":"0.003","completion":"0.004"}`, "", false, true, 0},
		{"unknown fallback price accepts explicit cover", `null`, `{"prompt":"0.003","completion":"0.004"}`, "", true, false, 3.04},
		{"invalid flex overrides cannot hide known fallback floor", `{"prompt":"0.001","completion":"0.002","input_cache_write":"0.005"}`, `{"prompt":"0.0001","completion":"0.0002","overrides":"invalid"}`, "", true, false, 5.02},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoints := []domain.NativePricedEndpoint{
				{Tag: "fallback/base", Pricing: endpointPricing(t, deps, tc.fallbackPricing)},
				{Tag: "first/flex", Status: &firstStatus, Pricing: endpointPricing(t, deps, tc.flexPricing)},
			}
			if tc.secondFlexPricing != "" {
				endpoints = append(endpoints, domain.NativePricedEndpoint{Tag: "second/flex", Status: &secondStatus, Pricing: endpointPricing(t, deps, tc.secondFlexPricing)})
			}
			in := textInput("test/model", endpoints...)
			in.ServiceTier = "flex"
			if tc.local {
				in.ConfiguredPrice, in.ApproximateReserveUSD = true, &localUSD
			}
			estimate, err := liveOnly(deps).Execute(in)
			if tc.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, estimate.USD, 1e-9)
		})
	}
}

func TestEndpointOneHourCacheOverrideAndWholeEndpointBound(t *testing.T) {
	deps := pricingadapter.Capabilities()
	in := textInput("test/model",
		domain.NativePricedEndpoint{Tag: "provider-a", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.1"`), "input_cache_write": json.RawMessage(`"0.002"`), "input_cache_write_1h": json.RawMessage(`"0.004"`), "overrides": json.RawMessage(`[{"min_prompt_tokens":1000,"input_cache_write_1h":"0.006","completion":"0.15"}]`)}},
		domain.NativePricedEndpoint{Tag: "provider-b", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.002"`), "completion": json.RawMessage(`"0.2"`), "input_cache_write_1h": json.RawMessage(`"0.003"`)}},
	)
	for _, scenario := range []struct {
		prompt int
		want   float64
	}{{999, 4.997}, {1000, 7.5}} {
		in.PromptTokens = scenario.prompt
		estimate, err := liveOnly(deps).Execute(in)
		require.NoError(t, err)
		assert.InDelta(t, scenario.want, estimate.USD, 1e-12)
	}
}

func TestEndpointOneHourCacheStrictRateAndUnknownDimensions(t *testing.T) {
	deps := pricingadapter.Capabilities()
	for _, scenario := range []struct {
		name, key, rate string
		want            float64
		fail            bool
	}{
		{"optional_absent", "", "", 1.27, false},
		{"explicit_zero", "input_cache_write_1h", `"0"`, 1.27, false},
		{"invalid", "input_cache_write_1h", `"invalid"`, 0, true},
		{"negative", "input_cache_write_1h", `"-0.1"`, 0, true},
		{"null", "input_cache_write_1h", `null`, 0, true},
		{"nan", "input_cache_write_1h", `"NaN"`, 0, true},
		{"infinity", "input_cache_write_1h", `"Infinity"`, 0, true},
		{"unknown_nonzero", "input_cache_write_2h", `"0.003"`, 0, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			prices := map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "input_cache_write": json.RawMessage(`"0.00125"`)}
			if scenario.key != "" {
				prices[scenario.key] = json.RawMessage(scenario.rate)
			}
			estimate, err := liveOnly(deps).Execute(textInput("test/model", domain.NativePricedEndpoint{Tag: "provider", MaxCompletionTokens: 10, Pricing: prices}))
			if scenario.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, scenario.want, estimate.USD, 1e-12)
		})
	}
}

func TestEndpointCalendarAdmissionWholeReachableTariffs(t *testing.T) {
	deps := pricingadapter.Capabilities()
	for _, scenario := range []struct {
		name, overrides string
		want            float64
	}{
		{"daily opposite rates exclude unreachable base", `[{"utc_start":0,"utc_end":1200,"prompt":"0.002","completion":"0.001"},{"utc_start":1200,"utc_end":0,"prompt":"0.001","completion":"0.05"}]`, 2.01},
		{"overlapping overrides combine in declaration order", `[{"utc_start":0,"utc_end":1200,"prompt":"0.003"},{"utc_start":1000,"utc_end":1400,"completion":"0.2"}]`, 5},
		{"weekend whole day plus wrapping weekday", `[{"utc_days":["saturday","sunday"],"prompt":"0.004","completion":"0.001"},{"utc_start":2300,"utc_end":100,"utc_days":["monday"],"prompt":"0.002","completion":"0.03"}]`, 4.01},
		{"calendar threshold excluded below prompt", `[{"min_prompt_tokens":1001,"utc_start":0,"utc_end":1200,"prompt":"0.01"},{"utc_start":1200,"utc_end":0,"prompt":"0.002","completion":"0.001"}]`, 2.01},
		{"calendar threshold equality retains actual base", `[{"min_prompt_tokens":1000,"utc_start":0,"utc_end":1200,"prompt":"0.003","completion":"0.002"}]`, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			estimate, err := liveOnly(deps).Execute(textInput("test/model", domain.NativePricedEndpoint{Tag: "provider", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.1"`), "overrides": json.RawMessage(scenario.overrides)}}))
			require.NoError(t, err)
			assert.InDelta(t, scenario.want, estimate.USD, 1e-12)
		})
	}
}

func TestEndpointCalendarAdmissionRejectsMalformedConditions(t *testing.T) {
	deps := pricingadapter.Capabilities()
	for _, declaration := range []string{
		`{"utc_start":1200,"prompt":"0.002"}`,
		`{"utc_start":null,"utc_end":1400,"prompt":"0.002"}`,
		`{"min_prompt_tokens":null,"utc_days":["monday"],"prompt":"0.002"}`,
		`{"utc_start":1260,"utc_end":1400,"prompt":"0.002"}`,
		`{"utc_start":2400,"utc_end":1400,"prompt":"0.002"}`,
		`{"utc_start":1200,"utc_end":1200,"prompt":"0.002"}`,
		`{"utc_start":"1200","utc_end":1400,"prompt":"0.002"}`,
		`{"utc_days":[],"prompt":"0.002"}`,
		`{"utc_days":["Monday"],"prompt":"0.002"}`,
		`{"utc_days":["monday","monday"],"prompt":"0.002"}`,
		`{"utc_days":null,"prompt":"0.002"}`,
		`{"utc_days":["monday"],"prompt":null}`,
		`{"utc_days":["monday"],"prompt":"-1"}`,
		`{"utc_days":["monday"],"unknown_charge":"1"}`,
		`{"utc_days":["monday"],"utc_month":1,"prompt":"0.002"}`,
		`{"prompt":"0.002"}`,
	} {
		t.Run(declaration, func(t *testing.T) {
			_, err := liveOnly(deps).Execute(textInput("test/model", domain.NativePricedEndpoint{Tag: "provider", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "overrides": json.RawMessage("[" + declaration + "]")}}))
			require.Error(t, err)
		})
	}
}

func TestEndpointSavedNativeCalendarAndGeminiMetadata(t *testing.T) {
	deps := pricingadapter.Capabilities()
	for _, scenario := range []struct {
		model, tag                         string
		contextLength, maxCompletionTokens int
		pricing                            string
		want                               float64
	}{
		{"deepseek/deepseek-v4-pro-0813", "deepseek", 1048576, 393216, `{"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022","discount":0,"overrides":[{"utc_days":["saturday","sunday"],"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022"},{"utc_start":0,"utc_end":100,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022"},{"utc_start":100,"utc_end":400,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000132","completion":"0.00000396","input_cache_read":"0.000000044"},{"utc_start":400,"utc_end":600,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022"},{"utc_start":600,"utc_end":1000,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000132","completion":"0.00000396","input_cache_read":"0.000000044"},{"utc_start":1000,"utc_end":0,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022"}]}`, 0.0013596},
		{"deepseek/deepseek-v4-flash-0731", "alibaba", 1000000, 393216, `{"prompt":"0.000000176","completion":"0.000000528","input_cache_read":"0.0000000176","discount":0,"overrides":[{"utc_start":0,"utc_end":1400,"prompt":"0.000000352","completion":"0.000001056","input_cache_read":"0.0000000352"},{"utc_start":1400,"utc_end":0,"prompt":"0.000000176","completion":"0.000000528","input_cache_read":"0.0000000176"}]}`, 0.00036256},
		{"tencent/hy3", "tencent/fp8", 262144, 128000, `{"prompt":"0.0000000825","completion":"0.00000033","input_cache_read":"0.000000020625","discount":0,"overrides":[{"utc_start":0,"utc_end":1600,"prompt":"0.000000132","completion":"0.000000528","input_cache_read":"0.000000033"},{"utc_start":1600,"utc_end":0,"prompt":"0.0000000825","completion":"0.00000033","input_cache_read":"0.000000020625"}]}`, 0.00013728},
		{"deepseek/deepseek-v4.1-flash", "deepseek", 1048576, 393216, `{"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003","discount":0,"overrides":[{"utc_days":["saturday","sunday"],"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003"},{"utc_start":0,"utc_end":100,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003"},{"utc_start":100,"utc_end":400,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.0000003","completion":"0.0000012","input_cache_read":"0.000000006"},{"utc_start":400,"utc_end":600,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003"},{"utc_start":600,"utc_end":1000,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.0000003","completion":"0.0000012","input_cache_read":"0.000000006"},{"utc_start":1000,"utc_end":0,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003"}]}`, 0.000312},
		{"tencent/hy4-preview", "tencent/fp8", 1048576, 64000, `{"prompt":"0.0000007506","completion":"0.0000022509","input_cache_read":"0.0000000378","discount":0,"overrides":[{"utc_start":0,"utc_end":1600,"prompt":"0.000000834","completion":"0.000002501","input_cache_read":"0.000000042"},{"utc_start":1600,"utc_end":0,"prompt":"0.0000007506","completion":"0.0000022509","input_cache_read":"0.0000000378"}]}`, 0.00085901},
		{"google/gemini-2.5-pro", "google-ai-studio", 1048576, 65536, `{"prompt":"0.00000125","completion":"0.00001","image":"0.00000125","audio":"0.00000125","input_audio_cache":"0.000000125","web_search":"0.014","internal_reasoning":"0.00001","input_cache_read":"0.000000125","input_cache_write":"0.000000375","discount":0,"overrides":[{"min_prompt_tokens":200000,"prompt":"0.0000025","completion":"0.000015","audio":"0.0000025","input_audio_cache":"0.00000025","input_cache_read":"0.00000025"}]}`, 0.00135},
	} {
		t.Run(scenario.model, func(t *testing.T) {
			endpoint := domain.NativePricedEndpoint{ModelID: scenario.model, Tag: scenario.tag, ContextLength: scenario.contextLength, MaxCompletionTokens: scenario.maxCompletionTokens, Pricing: endpointPricing(t, deps, scenario.pricing)}
			estimate, err := liveOnly(deps).Execute(textInput(scenario.model, endpoint))
			require.NoError(t, err)
			assert.InDelta(t, scenario.want, estimate.USD, 1e-12)
		})
	}
}

func TestEndpointGeminiAudioMetadataStrictRates(t *testing.T) {
	deps := pricingadapter.Capabilities()
	for _, key := range []string{"audio", "input_audio_cache"} {
		for _, rate := range []string{`"0"`, `null`, `"-1"`, `"NaN"`, `"Infinity"`} {
			t.Run(key+"/"+rate, func(t *testing.T) {
				prices := map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), key: json.RawMessage(rate)}
				estimate, err := liveOnly(deps).Execute(textInput("google/gemini-2.5-pro", domain.NativePricedEndpoint{ModelID: "google/gemini-2.5-pro", Tag: "google-ai-studio", MaxCompletionTokens: 10, Pricing: prices}))
				if rate != `"0"` {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				assert.InDelta(t, 1.02, estimate.USD, 1e-12)
			})
		}
	}
}

// The reserve of every routable model is priced from the fresh catalog
// document of its tariff model on the official OpenRouter API, otherwise from
// live, and from a stale document only when live fails. A missing catalog
// price or an empty pool is checked against live once; when live fails, the
// missing price falls to the local cover and the empty pool keeps its error.
func TestExecuteSourceOrder(t *testing.T) {
	deps := pricingadapter.Capabilities()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	endpoint := func(tag, prompt, completion string) domain.NativePricedEndpoint {
		return domain.NativePricedEndpoint{Tag: tag, MaxCompletionTokens: 100, Pricing: endpointPricing(t, deps, `{"prompt":"`+prompt+`","completion":"`+completion+`"}`)}
	}
	response := func(endpoints ...domain.NativePricedEndpoint) domain.NativeEndpointResponse {
		return domain.NativeEndpointResponse{Endpoints: endpoints, OutputModalities: []string{"text"}}
	}
	// The catalog prices a model at 1.02, live at 2.04.
	catalogPriced := response(endpoint("catalog-provider", "0.001", "0.002"), endpoint("catalog-provider/fast", "0.003", "0.006"))
	livePriced := response(endpoint("live-provider", "0.002", "0.004"))
	unpriced := response(domain.NativePricedEndpoint{Tag: "catalog-provider", MaxCompletionTokens: 100, Pricing: endpointPricing(t, deps, `{"prompt":"0.001"}`)})
	localUSD := .5
	for _, tc := range []struct {
		name      string
		models    []string
		batch     bool
		baseURL   string
		age       time.Duration
		catalog   map[string]domain.NativeEndpointResponse
		live      map[string]domain.NativeEndpointResponse
		only      []string
		local     bool
		wantUSD   float64
		wantErr   string
		source    domain.ReserveSource
		liveAsked []string
	}{
		{name: "fresh catalog", models: []string{"acme/a"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantUSD: 1.02, source: domain.ReserveSourceCatalog},
		{name: "shortcut prices its listed model", models: []string{"acme/a:nitro"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantUSD: 3.06, source: domain.ReserveSourceCatalog},
		{name: "batch prices the batch variant", models: []string{"acme/a"}, batch: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": livePriced, "acme/a:batch": catalogPriced}, wantUSD: 1.02, source: domain.ReserveSourceCatalog},
		{name: "one discovery per tariff model", models: []string{"acme/a", "acme/a:floor", "acme/b"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/b": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/b"}},
		{name: "model outside the catalog", models: []string{"acme/a"}, catalog: map[string]domain.NativeEndpointResponse{}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "channel outside the official API", models: []string{"acme/a"}, baseURL: "https://proxy.example/api", catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "stale catalog", models: []string{"acme/a"}, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "stale catalog when live fails", models: []string{"acme/a"}, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantUSD: 1.02, source: domain.ReserveSourceCatalogStale, liveAsked: []string{"acme/a"}},
		{name: "live fails without a document", models: []string{"acme/a"}, catalog: map[string]domain.NativeEndpointResponse{}, wantErr: "OpenRouter pricing discovery failed", liveAsked: []string{"acme/a"}},
		{name: "empty pool is checked against live", models: []string{"acme/a"}, only: []string{"live-provider"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "empty pool keeps its error when live fails", models: []string{"acme/a"}, only: []string{"live-provider"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantErr: "OpenRouter request has no compatible priced endpoint", liveAsked: []string{"acme/a"}},
		{name: "missing price is checked against live", models: []string{"acme/a"}, local: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": unpriced}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceConfiguredPrice, liveAsked: []string{"acme/a"}},
		{name: "missing price falls to the local cover when live fails", models: []string{"acme/a"}, local: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": unpriced}, wantUSD: .5, source: domain.ReserveSourceConfiguredPrice, liveAsked: []string{"acme/a"}},
		{name: "missing price is refused when live fails", models: []string{"acme/a"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": unpriced}, wantErr: "OpenRouter estimate unavailable: invalid OpenRouter endpoint price", liveAsked: []string{"acme/a"}},
		{name: "missing price of a stale document is not checked again", models: []string{"acme/a"}, local: true, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": unpriced}, wantUSD: .5, source: domain.ReserveSourceConfiguredPrice, liveAsked: []string{"acme/a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := &atomic.Pointer[domain.UpstreamCatalogTariffs]{}
			catalog.Store(&domain.UpstreamCatalogTariffs{Revision: "revision", FetchedAt: now.Add(-tc.age), Models: tc.catalog})
			live := &fakeLive{responses: tc.live}
			in := textInput(tc.models[len(tc.models)-1])
			in.Models, in.Batch, in.Live = tc.models, tc.batch, live
			in.ChannelBaseURL = "https://openrouter.ai/api/"
			if tc.baseURL != "" {
				in.ChannelBaseURL = tc.baseURL
			}
			in.Provider.Only = tc.only
			if tc.local {
				in.ConfiguredPrice, in.ApproximateReserveUSD = true, &localUSD
			}
			estimate, err := NewUsecase(deps, catalog, func() time.Time { return now }, func() int { return 24 }).Execute(in)
			assert.Equal(t, tc.liveAsked, live.asked)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.wantUSD, estimate.USD, 1e-9)
			assert.Equal(t, tc.source, estimate.Source)
		})
	}
}

// Batch prices the :batch variant without checking the output modalities it
// declares; a synchronous request checks them.
func TestExecuteBatchSkipsDeclaredOutputModalities(t *testing.T) {
	deps := pricingadapter.Capabilities()
	for _, batch := range []bool{false, true} {
		in := textInput("acme/a")
		in.Batch, in.Modalities = batch, []string{"text"}
		response := domain.NativeEndpointResponse{OutputModalities: []string{"embeddings"}, Endpoints: []domain.NativePricedEndpoint{
			{Tag: "provider", MaxCompletionTokens: 100, Pricing: endpointPricing(t, deps, `{"prompt":"0.001","completion":"0.002"}`)},
		}}
		in.Live = &fakeLive{responses: map[string]domain.NativeEndpointResponse{domain.UpstreamTariffModel("acme/a", batch): response}}
		estimate, err := liveOnly(deps).Execute(in)
		if !batch {
			require.EqualError(t, err, "OpenRouter model does not support requested output modality")
			continue
		}
		require.NoError(t, err)
		assert.InDelta(t, 1.02, estimate.USD, 1e-9)
	}
}
