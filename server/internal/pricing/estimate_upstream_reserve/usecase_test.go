package estimate_upstream_reserve

import (
	"encoding/json"
	"math"
	"testing"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// textInput is a nonstream text Chat request of 1000 prompt tokens and
// max_tokens 10 to model, routed to endpoints, as the gateway passes it.
func textInput(model string, endpoints ...domain.NativePricedEndpoint) Input {
	outputLimit := 10
	return Input{
		Model:               model,
		Models:              []string{model},
		Endpoints:           map[string][]domain.NativePricedEndpoint{model: endpoints},
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
			estimate, err := NewUsecase(deps).Execute(in)
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
		estimate, err := NewUsecase(deps).Execute(in)
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
			estimate, err := NewUsecase(deps).Execute(textInput("test/model", domain.NativePricedEndpoint{Tag: "provider", MaxCompletionTokens: 10, Pricing: prices}))
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
			estimate, err := NewUsecase(deps).Execute(textInput("test/model", domain.NativePricedEndpoint{Tag: "provider", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.1"`), "overrides": json.RawMessage(scenario.overrides)}}))
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
			_, err := NewUsecase(deps).Execute(textInput("test/model", domain.NativePricedEndpoint{Tag: "provider", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "overrides": json.RawMessage("[" + declaration + "]")}}))
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
			estimate, err := NewUsecase(deps).Execute(textInput(scenario.model, endpoint))
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
				estimate, err := NewUsecase(deps).Execute(textInput("google/gemini-2.5-pro", domain.NativePricedEndpoint{ModelID: "google/gemini-2.5-pro", Tag: "google-ai-studio", MaxCompletionTokens: 10, Pricing: prices}))
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
