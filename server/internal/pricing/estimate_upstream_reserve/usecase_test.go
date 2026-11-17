package estimate_upstream_reserve

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/shopspring/decimal"
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
	return NewUsecase(deps, &atomic.Pointer[domain.UpstreamCatalogTariffs]{}, time.Now, func() time.Duration { return 24 * time.Hour })
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

// The reserve of every routable model of text generation or embeddings is
// priced from the fresh catalog document of its tariff model on the official
// OpenRouter API, otherwise from live, and from a stale document only when live
// fails. A catalog model with a missing price, with no endpoint passing the
// filters or with an endpoint require_parameters excludes is checked against
// live once. When live fails, the document prices a missing price with the
// local cover, while an emptied or narrowed pool takes only the local cover
// and is otherwise refused with the live error.
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
	routedP := func(prompt, completion string) domain.NativeEndpointResponse {
		return response(endpoint("p", prompt, completion))
	}
	// One endpoint lists temperature, the other does not.
	partlyParameterized := response(endpoint("catalog-provider", "0.001", "0.002"), endpoint("catalog-provider/turbo", "0.003", "0.006"))
	partlyParameterized.Endpoints[0].SupportedParameters = []string{"temperature"}
	liveParameterized := response(endpoint("live-provider", "0.002", "0.004"))
	liveParameterized.Endpoints[0].SupportedParameters = []string{"temperature"}
	localUSD := .5
	for _, tc := range []struct {
		name    string
		models  []string
		batch   bool
		baseURL string
		age     time.Duration
		catalog map[string]domain.NativeEndpointResponse
		live    map[string]domain.NativeEndpointResponse
		only    []string
		local   bool
		// addOn sets the add-on reserve of the channel.
		addOn bool
		audio bool
		// temperature requires the temperature parameter of the endpoints.
		temperature bool
		wantUSD     float64
		wantErr     string
		source      domain.ReserveSource
		wantStale   bool
		liveAsked   []string
	}{
		{name: "fresh catalog", models: []string{"acme/a"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantUSD: 1.02, source: domain.ReserveSourceCatalog},
		{name: "shortcut prices its listed model", models: []string{"acme/a:nitro"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantUSD: 3.06, source: domain.ReserveSourceCatalog},
		{name: "batch prices the batch variant", models: []string{"acme/a"}, batch: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": livePriced, "acme/a:batch": catalogPriced}, wantUSD: 1.02, source: domain.ReserveSourceCatalog},
		{name: "one discovery per tariff model", models: []string{"acme/a", "acme/a:floor", "acme/b"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/b": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/b"}},
		{name: "model outside the catalog", models: []string{"acme/a"}, catalog: map[string]domain.NativeEndpointResponse{}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "channel outside the official API", models: []string{"acme/a"}, baseURL: "https://proxy.example/api", catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "stale catalog", models: []string{"acme/a"}, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "stale catalog when live fails", models: []string{"acme/a"}, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantUSD: 1.02, source: domain.ReserveSourceCatalogStale, wantStale: true, liveAsked: []string{"acme/a"}},
		{name: "live fails without a document", models: []string{"acme/a"}, catalog: map[string]domain.NativeEndpointResponse{}, wantErr: "OpenRouter pricing discovery failed", liveAsked: []string{"acme/a"}},
		{name: "empty pool is checked against live", models: []string{"acme/a"}, only: []string{"live-provider"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "empty pool is refused when live fails", models: []string{"acme/a"}, only: []string{"live-provider"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantErr: "OpenRouter pricing discovery failed", liveAsked: []string{"acme/a"}},
		{name: "empty pool takes the local cover when live fails", models: []string{"acme/a"}, only: []string{"live-provider"}, local: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantUSD: .5, source: domain.ReserveSourceConfiguredPrice, liveAsked: []string{"acme/a"}},
		{name: "empty pool of one model is refused when live fails, not priced by the other", models: []string{"acme/a", "acme/b"}, only: []string{"p"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced, "acme/b": routedP("0.001", "0.002")}, wantErr: "OpenRouter pricing discovery failed", liveAsked: []string{"acme/a"}},
		{name: "missing price is checked against live", models: []string{"acme/a"}, local: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": unpriced}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: 2.04, source: domain.ReserveSourceConfiguredPrice, liveAsked: []string{"acme/a"}},
		{name: "missing price falls to the local cover when live fails", models: []string{"acme/a"}, local: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": unpriced}, wantUSD: .5, source: domain.ReserveSourceConfiguredPrice, liveAsked: []string{"acme/a"}},
		{name: "missing price is refused when live fails", models: []string{"acme/a"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": unpriced}, wantErr: "OpenRouter estimate unavailable: invalid OpenRouter endpoint price", liveAsked: []string{"acme/a"}},
		{name: "missing price of a stale document is not checked again", models: []string{"acme/a"}, local: true, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": unpriced}, wantUSD: .5, source: domain.ReserveSourceConfiguredPrice, wantStale: true, liveAsked: []string{"acme/a"}},
		{name: "only the model with an empty pool is checked against live", models: []string{"acme/a", "acme/b"}, only: []string{"p"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced, "acme/b": routedP("0.001", "0.002")}, live: map[string]domain.NativeEndpointResponse{"acme/a": routedP("0.0005", "0.001")}, wantUSD: 1.02, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "a live check takes the maximum of both models", models: []string{"acme/a", "acme/b"}, only: []string{"p"}, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced, "acme/b": routedP("0.001", "0.002")}, live: map[string]domain.NativeEndpointResponse{"acme/a": routedP("0.002", "0.004")}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "an endpoint require_parameters excludes is checked against live", models: []string{"acme/a"}, temperature: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": partlyParameterized}, live: map[string]domain.NativeEndpointResponse{"acme/a": liveParameterized}, wantUSD: 2.04, source: domain.ReserveSourceLive, liveAsked: []string{"acme/a"}},
		{name: "empty pool of a stale document is refused", models: []string{"acme/a"}, only: []string{"p"}, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantErr: "OpenRouter pricing discovery failed", liveAsked: []string{"acme/a"}},
		{name: "empty pool of a stale document takes the local cover", models: []string{"acme/a"}, only: []string{"p"}, local: true, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantUSD: .5, source: domain.ReserveSourceConfiguredPrice, wantStale: true, liveAsked: []string{"acme/a"}},
		{name: "empty pool of one stale model is refused, not priced by the other", models: []string{"acme/a", "acme/b"}, only: []string{"p"}, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/b": routedP("0.001", "0.002")}, wantErr: "OpenRouter pricing discovery failed", liveAsked: []string{"acme/a", "acme/b"}},
		{name: "empty pool with an add-on reserve is an unavailable estimate when live fails", models: []string{"acme/a"}, only: []string{"live-provider"}, addOn: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantErr: "OpenRouter estimate unavailable: OpenRouter pricing discovery failed", liveAsked: []string{"acme/a"}},
		{name: "require_parameters narrowing is refused when live fails", models: []string{"acme/a"}, temperature: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": partlyParameterized}, wantErr: "OpenRouter pricing discovery failed", liveAsked: []string{"acme/a"}},
		{name: "require_parameters narrowing takes the local cover when live fails", models: []string{"acme/a"}, temperature: true, local: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": partlyParameterized}, wantUSD: .5, source: domain.ReserveSourceConfiguredPrice, liveAsked: []string{"acme/a"}},
		{name: "audio stays live", models: []string{"acme/a"}, audio: true, local: true, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, live: map[string]domain.NativeEndpointResponse{"acme/a": livePriced}, wantUSD: .5, source: domain.ReserveSourceConfiguredPrice, liveAsked: []string{"acme/a"}},
		{name: "a stale document is reported under a configured price", models: []string{"acme/a"}, local: true, age: 25 * time.Hour, catalog: map[string]domain.NativeEndpointResponse{"acme/a": catalogPriced}, wantUSD: 1.02, source: domain.ReserveSourceConfiguredPrice, wantStale: true, liveAsked: []string{"acme/a"}},
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
			if tc.addOn {
				in.AddOnReserveUSD = &localUSD
			}
			if tc.audio {
				in.Audio, in.TextOutput = true, false
			}
			if tc.temperature {
				in.Provider.RequireParameters = true
				in.Parameters = map[string]json.RawMessage{"temperature": json.RawMessage(`1`)}
			}
			estimate, err := NewUsecase(deps, catalog, func() time.Time { return now }, func() time.Duration { return 24 * time.Hour }).Execute(in)
			assert.Equal(t, tc.liveAsked, live.asked)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				if tc.addOn {
					var unavailable *domain.EstimateUnavailableError
					require.ErrorAs(t, err, &unavailable)
					assert.EqualError(t, unavailable.Cause, "OpenRouter pricing discovery failed")
				}
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.wantUSD, estimate.USD, 1e-9)
			assert.Equal(t, tc.source, estimate.Source)
			assert.Equal(t, tc.wantStale, estimate.StaleCatalog)
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

// Requested searches cost the web_search price of the endpoint each, without
// the add-on reserve. A price missing from the fresh catalog is checked against
// live; when live has none either, the search reserve covers the searches, and
// without it they are refused.
func TestExecutePricesSearches(t *testing.T) {
	deps := pricingadapter.Capabilities()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	response := func(prompt, completion, search string) domain.NativeEndpointResponse {
		pricing := `{"prompt":"` + prompt + `","completion":"` + completion + `"`
		if search != "" {
			pricing += `,"web_search":` + search
		}
		return domain.NativeEndpointResponse{OutputModalities: []string{"text"}, Endpoints: []domain.NativePricedEndpoint{
			{Tag: "provider", ModelID: "acme/a", MaxCompletionTokens: 100, Pricing: endpointPricing(t, deps, pricing+`}`)},
		}}
	}
	reserve := .5
	for _, tc := range []struct {
		name                string
		searches            int
		catalog             domain.NativeEndpointResponse
		live                map[string]domain.NativeEndpointResponse
		manualSearch        bool
		searchReserve       bool
		addOnReserve        bool
		wantUSD             float64
		wantErr             string
		wantSource          domain.ReserveSource
		wantIncludesReserve bool
		wantLiveAsked       []string
	}{
		{name: "searches cost the catalog price each", searches: 3, catalog: response("0.001", "0.002", `"0.01"`), wantUSD: 1.05, wantSource: domain.ReserveSourceCatalog},
		{name: "priced searches take no add-on reserve", searches: 3, catalog: response("0.001", "0.002", `"0.01"`), addOnReserve: true, wantUSD: 1.05, wantSource: domain.ReserveSourceCatalog},
		{name: "priced searches take no search reserve", searches: 3, catalog: response("0.001", "0.002", `"0.01"`), searchReserve: true, wantUSD: 1.05, wantSource: domain.ReserveSourceCatalog},
		{name: "a search price missing from the catalog is checked against live", searches: 3, catalog: response("0.001", "0.002", ""), live: map[string]domain.NativeEndpointResponse{"acme/a": response("0.002", "0.004", `"0.01"`)}, wantUSD: 2.07, wantSource: domain.ReserveSourceLive, wantLiveAsked: []string{"acme/a"}},
		{name: "a search price missing from live takes the search reserve", searches: 3, catalog: response("0.001", "0.002", ""), live: map[string]domain.NativeEndpointResponse{"acme/a": response("0.002", "0.004", "")}, searchReserve: true, wantUSD: 2.54, wantSource: domain.ReserveSourceLive, wantIncludesReserve: true, wantLiveAsked: []string{"acme/a"}},
		{name: "a missing search price takes the search reserve when live fails", searches: 3, catalog: response("0.001", "0.002", ""), searchReserve: true, wantUSD: 1.52, wantSource: domain.ReserveSourceCatalog, wantIncludesReserve: true, wantLiveAsked: []string{"acme/a"}},
		{name: "a missing search price without a search reserve is refused", searches: 3, catalog: response("0.001", "0.002", ""), live: map[string]domain.NativeEndpointResponse{"acme/a": response("0.002", "0.004", "")}, wantErr: domain.ErrHostedSearchUnpriced.Error(), wantLiveAsked: []string{"acme/a"}},
		{name: "a null search price is missing", searches: 1, catalog: response("0.001", "0.002", "null"), live: map[string]domain.NativeEndpointResponse{"acme/a": response("0.002", "0.004", `"0.01"`)}, wantUSD: 2.05, wantSource: domain.ReserveSourceLive, wantLiveAsked: []string{"acme/a"}},
		{name: "an invalid search price is refused", searches: 1, catalog: response("0.001", "0.002", `"-1"`), searchReserve: true, wantErr: "invalid OpenRouter endpoint price"},
		{name: "a search on an engine without an endpoint price takes the search reserve", manualSearch: true, catalog: response("0.001", "0.002", `"0.01"`), searchReserve: true, wantUSD: 1.52, wantSource: domain.ReserveSourceCatalog, wantIncludesReserve: true},
		{name: "native searches and a search on another engine add both", searches: 3, manualSearch: true, catalog: response("0.001", "0.002", `"0.01"`), searchReserve: true, wantUSD: 1.55, wantSource: domain.ReserveSourceCatalog, wantIncludesReserve: true},
		{name: "a search on another engine without a search reserve is refused", manualSearch: true, catalog: response("0.001", "0.002", `"0.01"`), wantErr: domain.ErrHostedSearchUnpriced.Error()},
		{name: "without requested searches the add-on reserve covers native search", catalog: response("0.001", "0.002", `"0.01"`), addOnReserve: true, wantUSD: 1.52, wantSource: domain.ReserveSourceCatalog, wantIncludesReserve: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := &atomic.Pointer[domain.UpstreamCatalogTariffs]{}
			catalog.Store(&domain.UpstreamCatalogTariffs{Revision: "revision", FetchedAt: now, Models: map[string]domain.NativeEndpointResponse{"acme/a": tc.catalog}})
			live := &fakeLive{responses: tc.live}
			in := textInput("acme/a")
			in.Live, in.ChannelBaseURL, in.Searches, in.ManualSearch = live, "https://openrouter.ai/api", tc.searches, tc.manualSearch
			if tc.searchReserve {
				in.SearchReserveUSD = &reserve
			}
			if tc.addOnReserve {
				in.AddOnReserveUSD = &reserve
			}
			estimate, err := NewUsecase(deps, catalog, func() time.Time { return now }, func() time.Duration { return 24 * time.Hour }).Execute(in)
			assert.Equal(t, tc.wantLiveAsked, live.asked)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.wantUSD, estimate.USD, 1e-9)
			assert.Equal(t, tc.wantSource, estimate.Source)
			assert.Equal(t, tc.wantIncludesReserve, estimate.IncludesAddOnReserve)
		})
	}
}

// A speech or rerank request is priced from the unit prices of the endpoints
// of a fresh catalog document: every non-zero price times its measured
// quantity, a million_tokens price per million tokens, output audio tokens at
// the output cap of the endpoint (the default output bound without one), the
// maximum over the endpoints. A rerank request measures both Cohere search
// units and input tokens; each endpoint bills the dimension it prices. An
// unknown price, a non-zero price of an unmeasured dimension or an endpoint
// without unit prices is covered only by the manual reserve, without a live
// check; a manual reserve below the priced cost is raised to it.
func TestExecuteUnitCost(t *testing.T) {
	deps := pricingadapter.Capabilities()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	characters := domain.PriceDimension{Billable: domain.PriceBillableOutputSpeech, Unit: domain.PriceUnitCharacter}
	seconds := domain.PriceDimension{Billable: domain.PriceBillableOutputAudio, Unit: domain.PriceUnitSecond}
	inputTokens := domain.PriceDimension{Billable: domain.PriceBillableInputText, Unit: domain.PriceUnitMillionTokens}
	audioTokens := domain.PriceDimension{Billable: domain.PriceBillableOutputAudio, Unit: domain.PriceUnitMillionTokens}
	searches := domain.PriceDimension{Billable: domain.PriceBillableSearch, Unit: domain.PriceUnitSearch}
	price := func(dimension domain.PriceDimension, usd string) domain.EndpointUnitPrice {
		if usd == "" {
			return domain.EndpointUnitPrice{Dimension: dimension}
		}
		amount := decimal.RequireFromString(usd)
		return domain.EndpointUnitPrice{Dimension: dimension, USD: &amount}
	}
	endpoint := func(tag string, prices ...domain.EndpointUnitPrice) domain.NativePricedEndpoint {
		return domain.NativePricedEndpoint{Tag: tag, ModelID: "acme/tts", Pricing: endpointPricing(t, deps, `{"prompt":"0","completion":"0","discount":0}`), UnitPrices: prices}
	}
	units := map[domain.PriceDimension]int64{characters: 1000, seconds: 30, inputTokens: 1000}
	rerankUnits := map[domain.PriceDimension]int64{searches: 2, inputTokens: 30000}
	// gemini is the real v13 document of google/gemini-3.8-flash-tts: input
	// $0.5 and output audio $9 per million tokens, output cap 16384.
	raw, err := os.ReadFile("../domain/testdata/catalog_v13/google__gemini-3.8-flash-tts.json")
	require.NoError(t, err)
	var document domain.SourceDocument
	require.NoError(t, deps.Codec.Unmarshal(raw, &document))
	gemini := domain.CatalogEndpointTariffs(deps, map[string]*domain.SourceDocument{"acme/tts": &document})["acme/tts"]
	require.Len(t, gemini.Endpoints, 2)
	// heygen is the real v13 document of heygen/heygen-voice-1: prompt mirrors
	// its $0.00003 character price after the 0.5 endpoint discount.
	raw, err = os.ReadFile("../domain/testdata/catalog_v13/heygen__heygen-voice-1.json")
	require.NoError(t, err)
	var heygenDocument domain.SourceDocument
	require.NoError(t, deps.Codec.Unmarshal(raw, &heygenDocument))
	heygen := domain.CatalogEndpointTariffs(deps, map[string]*domain.SourceDocument{"acme/tts": &heygenDocument})["acme/tts"]
	withPricing := func(endpoint domain.NativePricedEndpoint, pricing string) domain.NativePricedEndpoint {
		endpoint.Pricing = endpointPricing(t, deps, pricing)
		return endpoint
	}
	manual, lowManual := .5, .01
	for _, tc := range []struct {
		name      string
		endpoints []domain.NativePricedEndpoint
		units     map[domain.PriceDimension]int64
		manual    *float64
		rerank    bool
		wantUSD   float64
		source    domain.ReserveSource
		wantErr   string
	}{
		{name: "per character", endpoints: []domain.NativePricedEndpoint{endpoint("deepgram", price(characters, "0.00003"))}, wantUSD: .03, source: domain.ReserveSourceCatalog},
		{name: "output audio tokens without an output cap take the default bound", endpoints: []domain.NativePricedEndpoint{endpoint("google", price(inputTokens, "0.5"), price(audioTokens, "9"))}, wantUSD: .074228, source: domain.ReserveSourceCatalog},
		{name: "output audio tokens take the output cap of the endpoint", endpoints: gemini.Endpoints, wantUSD: .147956, source: domain.ReserveSourceCatalog},
		{name: "maximum over the endpoints", endpoints: []domain.NativePricedEndpoint{endpoint("deepgram", price(characters, "0.00003")), endpoint("seed", price(seconds, "0.0025"))}, wantUSD: .075, source: domain.ReserveSourceCatalog},
		{name: "a zero price of an unmeasured dimension bills nothing", endpoints: []domain.NativePricedEndpoint{endpoint("deepgram", price(characters, "0.00003"), price(domain.PriceDimension{}, "0"))}, wantUSD: .03, source: domain.ReserveSourceCatalog},
		{name: "a measured zero quantity bills nothing", endpoints: []domain.NativePricedEndpoint{endpoint("deepgram", price(characters, "0.00003"))}, units: map[domain.PriceDimension]int64{characters: 0}, wantUSD: 0, source: domain.ReserveSourceCatalog},
		{name: "a manual reserve below the cost is raised to it", endpoints: []domain.NativePricedEndpoint{endpoint("deepgram", price(characters, "0.00003"))}, manual: &lowManual, wantUSD: .03, source: domain.ReserveSourceManual},
		{name: "a manual reserve above the cost is kept", endpoints: []domain.NativePricedEndpoint{endpoint("deepgram", price(characters, "0.00003"))}, manual: &manual, wantUSD: .5, source: domain.ReserveSourceManual},
		{name: "an unknown price is refused", endpoints: []domain.NativePricedEndpoint{endpoint("elevenlabs", price(characters, "0.00003"), price(domain.PriceDimension{}, ""))}, wantErr: "OpenRouter estimate unavailable: OpenRouter endpoint unit price is unknown"},
		{name: "an unknown price takes the manual reserve", endpoints: []domain.NativePricedEndpoint{endpoint("elevenlabs", price(characters, ""))}, manual: &manual, wantUSD: .5, source: domain.ReserveSourceManual},
		{name: "a non-zero price of an unmeasured dimension is refused", endpoints: []domain.NativePricedEndpoint{endpoint("deepgram", price(characters, "0.00003"))}, units: map[domain.PriceDimension]int64{seconds: 30}, wantErr: "OpenRouter estimate unavailable: OpenRouter unit quantity is unmeasured: output_speech/character"},
		{name: "an endpoint without unit prices is refused", endpoints: []domain.NativePricedEndpoint{endpoint("deepgram")}, wantErr: "OpenRouter estimate unavailable: OpenRouter endpoint unit prices are unknown"},
		{name: "an endpoint without unit prices takes the manual reserve", endpoints: []domain.NativePricedEndpoint{endpoint("deepgram")}, manual: &manual, wantUSD: .5, source: domain.ReserveSourceManual},
		{name: "prompt mirroring a unit price after the endpoint discount", endpoints: heygen.Endpoints, wantUSD: .03, source: domain.ReserveSourceCatalog},
		{name: "a non-zero request rate is not covered by unit prices", endpoints: []domain.NativePricedEndpoint{withPricing(endpoint("deepgram", price(characters, "0.00003")), `{"prompt":"0.00003","request":"0.01"}`)}, wantErr: "OpenRouter estimate unavailable: OpenRouter endpoint price request is not covered by its unit prices"},
		{name: "an uncovered rate takes the manual reserve", endpoints: []domain.NativePricedEndpoint{withPricing(endpoint("deepgram", price(characters, "0.00003")), `{"request":"0.01"}`)}, manual: &manual, wantUSD: .5, source: domain.ReserveSourceManual},
		{name: "rerank prompt beyond the search price is not covered", endpoints: []domain.NativePricedEndpoint{withPricing(endpoint("cohere", price(searches, "0.002")), `{"prompt":"0.000001","completion":"0"}`)}, units: rerankUnits, rerank: true, wantErr: "OpenRouter estimate unavailable: OpenRouter endpoint price prompt is not covered by its unit prices"},
		{name: "rerank per search", endpoints: []domain.NativePricedEndpoint{endpoint("cohere", price(searches, "0.002"))}, units: rerankUnits, rerank: true, wantUSD: .004, source: domain.ReserveSourceCatalog},
		{name: "rerank per token", endpoints: []domain.NativePricedEndpoint{endpoint("voyageai", price(inputTokens, "0.05"))}, units: rerankUnits, rerank: true, wantUSD: .0015, source: domain.ReserveSourceCatalog},
		{name: "rerank maximum over search prices", endpoints: []domain.NativePricedEndpoint{endpoint("cohere", price(searches, "0.001")), endpoint("bedrock", price(searches, "0.002"))}, units: rerankUnits, rerank: true, wantUSD: .004, source: domain.ReserveSourceCatalog},
		{name: "rerank maximum over search and token prices", endpoints: []domain.NativePricedEndpoint{endpoint("cohere", price(searches, "0.001")), endpoint("mongodb", price(inputTokens, "0.1"))}, units: rerankUnits, rerank: true, wantUSD: .003, source: domain.ReserveSourceCatalog},
		{name: "rerank manual reserve below the cost is raised to it", endpoints: []domain.NativePricedEndpoint{endpoint("voyageai", price(inputTokens, "0.5"))}, units: rerankUnits, rerank: true, manual: &lowManual, wantUSD: .015, source: domain.ReserveSourceManual},
		{name: "rerank unknown price is refused", endpoints: []domain.NativePricedEndpoint{endpoint("cohere", price(searches, ""))}, units: rerankUnits, rerank: true, wantErr: "OpenRouter estimate unavailable: OpenRouter endpoint unit price is unknown"},
		{name: "rerank unknown price takes the manual reserve", endpoints: []domain.NativePricedEndpoint{endpoint("cohere", price(searches, ""))}, units: rerankUnits, rerank: true, manual: &manual, wantUSD: .5, source: domain.ReserveSourceManual},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := []string{"speech"}
			if tc.rerank {
				output = []string{"rerank"}
			}
			catalog := &atomic.Pointer[domain.UpstreamCatalogTariffs]{}
			catalog.Store(&domain.UpstreamCatalogTariffs{FetchedAt: now, Models: map[string]domain.NativeEndpointResponse{
				"acme/tts": {ID: "acme/tts", Endpoints: tc.endpoints, OutputModalities: output},
			}})
			live := &fakeLive{}
			in := Input{
				Model: "acme/tts", Models: []string{"acme/tts"}, ChannelBaseURL: "https://openrouter.ai/api", Live: live,
				Modalities: []string{"speech"}, Count: 1, Units: units, DefaultOutputTokens: 8192, ApproximateReserveUSD: tc.manual,
			}
			if tc.units != nil {
				in.Units = tc.units
			}
			if tc.rerank {
				// The gateway passes a rerank request without output modalities.
				in.Rerank, in.Modalities = true, nil
			}
			estimate, err := NewUsecase(deps, catalog, func() time.Time { return now }, func() time.Duration { return 24 * time.Hour }).Execute(in)
			assert.Empty(t, live.asked)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				var unavailable *domain.EstimateUnavailableError
				assert.ErrorAs(t, err, &unavailable)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.wantUSD, estimate.USD, 1e-12)
			assert.Equal(t, tc.source, estimate.Source)
		})
	}
}

// A transcription is reserved at its seconds × the maximum per-second price
// over the endpoints of its catalog document, although the endpoint prompt
// mirrors another published amount of the dimension: a lower variant
// (assemblyai prompted, deepgram multilingual), the price of another
// provider endpoint, or the amount of a per-hour component (mai-transcribe).
// A prompt no component publishes is still refused. The documents are
// trimmed from the local catalog publication (normalization version 8).
func TestExecuteTranscriptionOnCatalogDocuments(t *testing.T) {
	deps := pricingadapter.Capabilities()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	seconds := domain.PriceDimension{Billable: domain.PriceBillableInputAudio, Unit: domain.PriceUnitSecond}
	for _, tc := range []struct {
		model, prompt string
		wantUSD       float64
		wantErr       string
	}{
		{model: "assemblyai/universal-3-6-pro", prompt: "0.00009", wantErr: "OpenRouter estimate unavailable: OpenRouter endpoint price prompt is not covered by its unit prices"},
		{"assemblyai/universal-3-5-pro", "", 60 * 0.0000972222, ""},
		{"assemblyai/universal-3-6-pro", "", 60 * 0.0000972222, ""},
		{"deepgram/nova-3", "", 60 * 0.0000866666666667, ""},
		{"openai/whisper-large-v3", "", 60 * 0.111 / 3600, ""},
		{"openai/whisper-large-v3-turbo", "", 60 * 0.04 / 3600, ""},
		{"mistralai/voxtral-mini-transcribe-3-0", "", 60 * 0.0000641667, ""},
		{"mistralai/voxtral-small-transcribe-3-0", "", 60 * 0.0044 / 60, ""},
		{"microsoft/mai-transcribe-1.5", "", 60 * 0.36 / 3600, ""},
		{"microsoft/mai-transcribe-2", "", 60 * 0.1 / 3600, ""},
	} {
		t.Run(tc.model+tc.prompt, func(t *testing.T) {
			raw, err := os.ReadFile("../domain/testdata/catalog_v8_transcription/" + strings.ReplaceAll(tc.model, "/", "__") + ".json")
			require.NoError(t, err)
			var document domain.SourceDocument
			require.NoError(t, deps.Codec.Unmarshal(raw, &document))
			if tc.prompt != "" {
				document.Offers[0].Facts = json.RawMessage(`{"pricing":{"prompt":"` + tc.prompt + `","completion":"0","discount":0}}`)
			}
			catalog := &atomic.Pointer[domain.UpstreamCatalogTariffs]{}
			catalog.Store(&domain.UpstreamCatalogTariffs{FetchedAt: now, Models: domain.CatalogEndpointTariffs(deps, map[string]*domain.SourceDocument{tc.model: &document})})
			live := &fakeLive{}
			estimate, err := NewUsecase(deps, catalog, func() time.Time { return now }, func() time.Duration { return 24 * time.Hour }).Execute(Input{
				Model: tc.model, Models: []string{tc.model}, ChannelBaseURL: "https://openrouter.ai/api", Live: live,
				Modalities: []string{"transcription"}, Count: 1, Units: map[domain.PriceDimension]int64{seconds: 60}, DefaultOutputTokens: 8192,
			})
			assert.Empty(t, live.asked)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.wantUSD, estimate.USD, 1e-9)
			assert.Equal(t, domain.ReserveSourceCatalog, estimate.Source)
		})
	}
}
