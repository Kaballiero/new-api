package openrouter

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	"github.com/QuantumNous/new-api/internal/app/pricingruntime"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Numeric endpoint status must not override provider routing or reservation.
func TestEndpointStatusRouting(t *testing.T) {
	for _, status := range []string{"absent", "0", "-1", "-2", "-3", "-5", "-10", "73"} {
		for _, tc := range []struct {
			name, pool, extra string
			want              float64
		}{
			{"sole", "expensive", "", 3.04},
			{"mixed", "mixed", "", 3.04},
			{"all same status", "all", "", 3.04},
			{"only negative", "mixed", `,"provider":{"only":["expensive"]}`, 3.04},
			{"ignore negative", "mixed", `,"provider":{"ignore":["expensive"]}`, 1.02},
			{"order retains fallback", "mixed", `,"provider":{"order":["cheap"]}`, 3.04},
			{"no fallback cheap", "mixed", `,"provider":{"order":["cheap"],"allow_fallbacks":false}`, 1.02},
			{"no fallback negative", "mixed", `,"provider":{"order":["expensive"],"allow_fallbacks":false}`, 3.04},
			{"max price cheap only", "mixed", `,"provider":{"max_price":{"prompt":2000}}`, 1.02},
			{"max price excludes all", "mixed", `,"provider":{"max_price":{"prompt":1}}`, 0},
			{"flex suppresses default", "flex", `,"service_tier":"flex"`, 3.04},
			{"cheaper flex lowers reserve", "cheapflex", `,"service_tier":"flex"`, 1.02},
			{"priority includes selected tier", "priority", `,"service_tier":"priority"`, 3.04},
		} {
			t.Run(status+"/"+tc.name, func(t *testing.T) {
				statusJSON := ""
				if status != "absent" {
					statusJSON = `,"status":` + status
				}
				var expensive, cheap PricedEndpoint
				require.NoError(t, common.Unmarshal([]byte(`{"tag":"expensive/base","context_length":10000,"max_completion_tokens":100,"pricing":{"prompt":"0.003","completion":"0.004"}`+statusJSON+`}`), &expensive))
				require.NoError(t, common.Unmarshal([]byte(`{"tag":"cheap/base","context_length":10000,"max_completion_tokens":100,"pricing":{"prompt":"0.001","completion":"0.002"}}`), &cheap))
				pool := []PricedEndpoint{cheap, expensive}
				switch tc.pool {
				case "expensive":
					pool = []PricedEndpoint{expensive}
				case "all":
					pool[0].Status = expensive.Status
				case "flex":
					pool[1].Tag = "expensive/flex"
				case "cheapflex":
					pool[1].Tag = "expensive/flex"
					pool[0].Pricing, pool[1].Pricing = pool[1].Pricing, pool[0].Pricing
				case "priority":
					pool[1].Tag = "expensive/fast"
				}
				e := NewRequestEstimator()
				e.Models["test/model"] = pool
				info := &relaycommon.RelayInfo{}
				info.SetEstimatePromptTokens(1000)
				cost, err := e.Estimate(info, []byte(`{"model":"test/model","max_tokens":10`+tc.extra+`}`), "chat/completions")
				want := tc.want
				if want == 0 {
					require.ErrorContains(t, err, "no compatible priced endpoint")
					return
				}
				require.NoError(t, err)
				assert.InDelta(t, want, cost, 1e-9)
			})
		}
	}
}

func TestEndpointStatusOperations(t *testing.T) {
	for _, status := range []int{0, -1, -2, -3, -5, -10, 73} {
		for _, tc := range []struct {
			name, operation string
			format          types.RelayFormat
			modality, extra string
			local           bool
			want            float64
		}{
			{"chat", "chat/completions", types.RelayFormatOpenAI, "text", `,"max_tokens":10`, false, 1.02},
			{"responses", "responses", types.RelayFormatOpenAIResponses, "text", `,"max_output_tokens":10`, false, 1.02},
			{"embeddings", "embeddings", types.RelayFormatOpenAI, "embeddings", "", false, 1},
			{"batch chat", "batch_chat/completions", types.RelayFormatOpenAI, "text", `,"max_tokens":10`, false, 1.02},
			{"batch responses", "batch_responses", types.RelayFormatOpenAIResponses, "text", `,"max_output_tokens":10`, false, 1.02},
			{"batch embeddings", "batch_embeddings", types.RelayFormatOpenAI, "embeddings", "", false, 1},
			{"rerank", "rerank", types.RelayFormatRerank, "rerank", `,"query":"hello","documents":["hello"]`, true, .25},
			{"tts", "audio/speech", types.RelayFormatOpenAIAudio, "speech", `,"input":"hello"`, true, .25},
			{"stt", "audio/transcriptions", types.RelayFormatOpenAIAudio, "transcription", "", true, .25},
			{"chat audio music", "chat/completions", types.RelayFormatOpenAI, "audio", `,"modalities":["audio"],"stream":true`, true, .25},
			{"decisions", "decisions", types.RelayFormatSystemOne, "decisions", `,"state":{},"questions":[]`, true, .25},
		} {
			t.Run(fmt.Sprintf("%d/%s", status, tc.name), func(t *testing.T) {
				e := NewRequestEstimator()
				var endpoint PricedEndpoint
				require.NoError(t, common.Unmarshal([]byte(fmt.Sprintf(`{"tag":"provider","model_id":"test/model","status":%d,"context_length":10000,"max_completion_tokens":100,"pricing":{"prompt":"0.001","completion":"0.002"}}`, status)), &endpoint))
				e.Models["test/model"] = []PricedEndpoint{endpoint}
				e.Models["test/model:batch"] = e.Models["test/model"]
				e.OutputModalities["test/model"] = []string{tc.modality}
				info := &relaycommon.RelayInfo{RelayFormat: tc.format, IsStream: tc.modality == "audio"}
				info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
				info.SetEstimatePromptTokens(1000)
				body := []byte(`{"model":"test/model"` + tc.extra + `}`)
				var cost float64
				var err error
				if tc.local {
					cost, err = e.EstimateWithLocalPrice(info, body, tc.operation, .25)
				} else {
					cost, err = e.Estimate(info, body, tc.operation)
				}
				require.NoError(t, err)
				assert.InDelta(t, tc.want, cost, 1e-9)
			})
		}
	}
}

func TestEndpointStatusRetainedGuards(t *testing.T) {
	for _, tc := range []struct {
		name, metadata, extra, operation string
		local                            bool
	}{
		{"context", `"context_length":999,`, `,"max_tokens":10`, "chat/completions", false},
		{"completion limit", `"max_completion_tokens":9,`, `,"max_tokens":10`, "chat/completions", false},
		{"invalid output cap", "", `,"max_tokens":2147483647`, "chat/completions", false},
		{"parameter", "", `,"max_tokens":10,"temperature":1,"provider":{"require_parameters":true}`, "chat/completions", false},
		{"quantization", "", `,"max_tokens":10,"provider":{"quantizations":["fp8"]}`, "chat/completions", false},
		{"unknown model", "", `,"models":["other/model"],"max_tokens":10`, "chat/completions", false},
		{"unknown modality", "", `,"modalities":["video"]`, "chat/completions", true},
		{"audio format", "", `,"modalities":["audio"],"stream":true`, "chat/completions", true},
		{"audio routing", "", `,"provider":{"only":["provider"]}`, "audio/speech", true},
		{"rerank contract", "", "", "rerank", false},
		{"batch local reserve", "", `,"max_tokens":10`, "batch_chat/completions", true},
		{"bad tariff", `"pricing":{"prompt":null},`, `,"max_tokens":10`, "chat/completions", false},
		{"unknown max price", "", `,"provider":{"max_price":{"prompt":null}},"max_tokens":10`, "chat/completions", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewRequestEstimator()
			var endpoint PricedEndpoint
			metadata := `{"tag":"provider","status":-2,` + tc.metadata + `"model_id":"test/model"}`
			require.NoError(t, common.Unmarshal([]byte(metadata), &endpoint))
			if tc.name != "bad tariff" {
				require.NoError(t, common.UnmarshalJsonStr(`{"prompt":"0.001","completion":"0.002"}`, &endpoint.Pricing))
			}
			e.Models["test/model"] = []PricedEndpoint{endpoint}
			e.OutputModalities["test/model"] = []string{"text"}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			info.SetEstimatePromptTokens(1000)
			body := []byte(`{"model":"test/model"` + tc.extra + `}`)
			var err error
			if tc.local {
				_, err = e.EstimateWithLocalPrice(info, body, tc.operation, .25)
			} else {
				_, err = e.Estimate(info, body, tc.operation)
			}
			require.Error(t, err)
		})
	}
}

// Without a client output limit the reserve covers min(endpoint cap, 8192)
// output tokens; an explicit limit of any format is reserved as given. The
// reserve names the limit field of the client's format.
func TestEndpointDefaultOutputCap(t *testing.T) {
	for _, tc := range []struct {
		name, limits, extra, operation, field string
		format                                types.RelayFormat
		explicit                              bool
		output, responses                     int
	}{
		{"no limit with endpoint cap 128000 uses 8192", `"context_length":200000,"max_completion_tokens":128000,`, "", "chat/completions", "max_tokens", types.RelayFormatOpenAI, false, 8192, 1},
		{"no limit with endpoint cap 4096 uses 4096", `"context_length":200000,"max_completion_tokens":4096,`, "", "chat/completions", "max_tokens", types.RelayFormatOpenAI, false, 4096, 1},
		{"no endpoint cap uses 8192", "", "", "chat/completions", "max_tokens", types.RelayFormatOpenAI, false, 8192, 1},
		{"context left after the prompt bounds the default cap", `"context_length":5000,"max_completion_tokens":128000,`, "", "chat/completions", "max_tokens", types.RelayFormatOpenAI, false, 4000, 1},
		{"n multiplies the output price", `"max_completion_tokens":128000,`, `,"n":2`, "chat/completions", "max_tokens", types.RelayFormatOpenAI, false, 8192, 2},
		{"explicit max_tokens is used as given", `"max_completion_tokens":128000,`, `,"max_tokens":20000`, "chat/completions", "max_tokens", types.RelayFormatOpenAI, true, 20000, 1},
		{"explicit max_completion_tokens is used as given", `"max_completion_tokens":128000,`, `,"max_completion_tokens":100`, "chat/completions", "max_tokens", types.RelayFormatOpenAI, true, 100, 1},
		{"Responses max_output_tokens is respected", `"max_completion_tokens":128000,`, `,"input":"hi","max_output_tokens":100`, "responses", "max_output_tokens", types.RelayFormatOpenAIResponses, true, 100, 1},
		{"Responses without a limit uses 8192", `"max_completion_tokens":128000,`, `,"input":"hi"`, "responses", "max_output_tokens", types.RelayFormatOpenAIResponses, false, 8192, 1},
		{"Gemini without a limit names maxOutputTokens", `"max_completion_tokens":128000,`, "", "chat/completions", "maxOutputTokens", types.RelayFormatGemini, false, 8192, 1},
		{"batch item without a limit is capped", `"max_completion_tokens":128000,`, "", "batch_chat/completions", "max_tokens", types.RelayFormatOpenAI, false, 8192, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var endpoint PricedEndpoint
			require.NoError(t, common.UnmarshalJsonStr(`{"tag":"provider","model_id":"test/model",`+tc.limits+`"pricing":{"prompt":"0.000001","completion":"0.000002"}}`, &endpoint))
			e := NewRequestEstimator()
			e.Models["test/model"] = []PricedEndpoint{endpoint}
			e.Models["test/model:batch"] = e.Models["test/model"]
			info := &relaycommon.RelayInfo{RelayFormat: tc.format}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			info.SetEstimatePromptTokens(1000)

			cost, err := e.Estimate(info, []byte(`{"model":"test/model"`+tc.extra+`}`), tc.operation)

			require.NoError(t, err)
			outputPrice := decimal.RequireFromString("0.000002").Mul(decimal.NewFromInt(int64(tc.responses)))
			want, _ := decimal.RequireFromString("0.001").Add(outputPrice.Mul(decimal.NewFromInt(int64(tc.output)))).Float64()
			assert.InDelta(t, want, cost, 1e-12)
			require.NotNil(t, e.OutputReserve)
			assert.Equal(t, tc.explicit, e.OutputReserve.ExplicitLimit)
			assert.Equal(t, tc.field, e.OutputReserve.LimitField)
			assert.Equal(t, tc.output, e.OutputReserve.OutputTokens)
			assert.Equal(t, "0.001", e.OutputReserve.InputUSD.String())
			assert.Equal(t, "0.000001", e.OutputReserve.PromptPriceUSD.String())
			assert.Equal(t, outputPrice.String(), e.OutputReserve.OutputPriceUSD.String())
		})
	}
}

// A configured price is a minimum hold: the endpoint estimate without a client
// limit also uses min(endpoint cap, 8192) and may raise the reserve above it.
func TestEndpointConfiguredPriceDefaultOutputCap(t *testing.T) {
	for _, tc := range []struct {
		name, limits string
		want         float64
	}{
		{"capped endpoint below the configured price", `"max_completion_tokens":4096,`, 0.01},
		{"no endpoint cap counts 8192 above the configured price", "", 0.001 + 0.000002*8192},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var endpoint PricedEndpoint
			require.NoError(t, common.UnmarshalJsonStr(`{"tag":"provider","model_id":"test/model",`+tc.limits+`"pricing":{"prompt":"0.000001","completion":"0.000002"}}`, &endpoint))
			e := NewRequestEstimator()
			e.Models["test/model"] = []PricedEndpoint{endpoint}
			e.OutputModalities["test/model"] = []string{"text"}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			info.SetEstimatePromptTokens(1000)

			cost, err := e.EstimateWithLocalPrice(info, []byte(`{"model":"test/model"}`), "chat/completions", 0.01)

			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-12)
			assert.Equal(t, ReserveWholeHold, e.ReserveUse)
		})
	}
}

// Sending the suggested limit in the client's field must yield a reserve that
// fits the quota the suggestion was computed for, although the field itself
// adds prompt tokens.
func TestEndpointAffordableLimitFitsReserve(t *testing.T) {
	const text = "Summarize the history of the Roman Empire from its founding to the fall of Constantinople, with dates."
	for _, tc := range []struct {
		name, operation, body, limitField string
		format                            types.RelayFormat
	}{
		{"chat", "chat/completions", `{"model":"test/model","messages":[{"role":"user","content":"` + text + `"}]%s}`, "max_tokens", types.RelayFormatOpenAI},
		{"Responses", "responses", `{"model":"test/model","input":"` + text + `"%s}`, "max_output_tokens", types.RelayFormatOpenAIResponses},
		// The estimator prices the converted Chat body, where Gemini's limit is max_tokens.
		{"Gemini", "chat/completions", `{"model":"test/model","messages":[{"role":"user","content":"` + text + `"}]%s}`, "max_tokens", types.RelayFormatGemini},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var endpoints []PricedEndpoint
			require.NoError(t, common.UnmarshalJsonStr(`[{"tag":"a","context_length":200000,"max_completion_tokens":128000,"pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_write":"0.00000375"}}]`, &endpoints))
			info := &relaycommon.RelayInfo{RelayFormat: tc.format}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			basis := common.OpenRouterCostBasis{EffectiveGroupRatio: 1, QuotaPerUnit: 500000}
			e := NewRequestEstimator()
			e.Models["test/model"] = endpoints
			estimate, err := e.Estimate(info, []byte(fmt.Sprintf(tc.body, "")), tc.operation)
			require.NoError(t, err)
			required, _, err := basis.Quota(estimate)
			require.NoError(t, err)
			reserve := *e.OutputReserve
			suggestions := 0
			for available := 0; available < required; available += required/200 + 1 {
				limit := reserve.AffordableMaxTokens(basis, available)
				if limit == 0 {
					continue
				}
				suggestions++
				limited, err := e.Estimate(info, []byte(fmt.Sprintf(tc.body, fmt.Sprintf(`,"%s":%d`, tc.limitField, limit))), tc.operation)
				require.NoError(t, err)
				quota, _, err := basis.Quota(limited)
				require.NoError(t, err)
				assert.LessOrEqual(t, quota, available, "limit %d", limit)
			}
			assert.Greater(t, suggestions, 150)
		})
	}
}

func TestEndpointStatusRequestDiscovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/models/test/model:batch/endpoints", r.URL.Path)
		gets++
		status := -2
		if gets > 1 {
			status = 0
		}
		_, err := fmt.Fprintf(w, `{"data":{"id":"test/model:batch","endpoints":[{"tag":"provider","status":%d,"max_completion_tokens":100,"pricing":{"prompt":"0.001","completion":"0.002"}}]}}`, status)
		assert.NoError(t, err)
	}))
	defer server.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}
	info.SetEstimatePromptTokens(1000)
	e := NewRequestEstimator()
	e.Bind(c, &imageTestAdaptor{})
	for range 3 {
		_, err := e.Estimate(info, []byte(`{"model":"test/model","max_tokens":10}`), "batch_chat/completions")
		require.NoError(t, err)
	}
	assert.Equal(t, 1, gets)
	assert.Equal(t, -2, *e.Models["test/model:batch"][0].Status)
	assert.Equal(t, pricingdomain.ReserveSourceLive, e.ReserveSource)
	next := NewRequestEstimator()
	require.NoError(t, next.Discover(c, info, &imageTestAdaptor{}, "test/model:batch"))
	assert.Equal(t, 2, gets)
	assert.Equal(t, 0, *next.Models["test/model:batch"][0].Status)
}

func TestEndpointStatusMonetaryBoundary(t *testing.T) {
	for _, policy := range []string{"strict", "positive local", "zero local"} {
		t.Run(policy, func(t *testing.T) {
			e := NewRequestEstimator()
			var endpoints []PricedEndpoint
			require.NoError(t, common.UnmarshalJsonStr(`[{"tag":"cheap","status":0,"max_completion_tokens":100,"pricing":{"prompt":"0.001","completion":"0.002"}},{"tag":"unknown-price","status":-2,"max_completion_tokens":100,"pricing":[]}]`, &endpoints))
			e.Models["test/model"] = endpoints
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			info.SetEstimatePromptTokens(1000)
			body := []byte(`{"model":"test/model","max_tokens":10}`)
			var cost float64
			var err error
			switch policy {
			case "strict":
				cost, err = e.Estimate(info, body, "chat/completions")
			case "positive local":
				cost, err = e.EstimateWithLocalPrice(info, body, "chat/completions", .25)
			case "zero local":
				cost, err = e.EstimateWithLocalPrice(info, body, "chat/completions", 0)
			}
			if policy != "positive local" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, 1.02, cost, 1e-9)
		})
	}
}

func TestEndpointFlexEligiblePool(t *testing.T) {
	for _, status := range []int{0, -2} {
		for _, reversed := range []bool{false, true} {
			for _, tc := range []struct {
				name, limits, extra string
				cacheWrite          bool
				want                float64
			}{
				{"eligible flex", "", "", false, 3.04},
				{"excluded by order", "", `,"provider":{"order":["cheap"],"allow_fallbacks":false}`, false, 1.02},
				{"ordered fallback remains eligible", "", `,"provider":{"order":["cheap"]}`, false, 3.04},
				{"excluded by only", "", `,"provider":{"only":["cheap"]}`, false, 1.02},
				{"excluded by ignore", "", `,"provider":{"ignore":["expensive"]}`, false, 1.02},
				{"excluded by context", `,"context_length":999`, "", false, 1.02},
				{"excluded by prompt limit", `,"max_prompt_tokens":999`, "", false, 1.02},
				{"excluded by completion limit", `,"max_completion_tokens":9`, "", false, 1.02},
				{"excluded by remaining context", `,"context_length":1009`, "", false, 1.02},
				{"excluded by max prompt price", "", `,"provider":{"max_price":{"prompt":2000}}`, false, 1.02},
				{"excluded by max completion price", "", `,"provider":{"max_price":{"completion":3000}}`, false, 1.02},
				{"excluded by quantization", `,"quantization":"fp8"`, `,"provider":{"quantizations":["bf16"]}`, false, 1.02},
				{"excluded by parameters", "", `,"provider":{"require_parameters":true}`, false, 1.02},
				{"fallback cache write retains complete maximum", "", "", true, 5.02},
			} {
				t.Run(fmt.Sprintf("%d/reversed=%t/%s", status, reversed, tc.name), func(t *testing.T) {
					var cheap, flex PricedEndpoint
					require.NoError(t, common.UnmarshalJsonStr(`{"tag":"cheap/base","quantization":"bf16","supported_parameters":["max_tokens","service_tier"],"pricing":{"prompt":"0.001","completion":"0.002"}}`, &cheap))
					require.NoError(t, common.UnmarshalJsonStr(fmt.Sprintf(`{"tag":"expensive/flex","status":%d,"pricing":{"prompt":"0.003","completion":"0.004"}%s}`, status, tc.limits), &flex))
					if tc.cacheWrite {
						cheap.Pricing["input_cache_write"] = []byte(`"0.005"`)
					}
					endpoints := []PricedEndpoint{cheap, flex}
					if reversed {
						endpoints[0], endpoints[1] = endpoints[1], endpoints[0]
					}
					e := NewRequestEstimator()
					e.Models["test/model"] = endpoints
					info := &relaycommon.RelayInfo{}
					info.SetEstimatePromptTokens(1000)
					cost, err := e.Estimate(info, []byte(`{"model":"test/model","max_tokens":10,"service_tier":"flex"`+tc.extra+`}`), "chat/completions")
					require.NoError(t, err)
					assert.InDelta(t, tc.want, cost, 1e-9)
				})
			}
		}
	}
}

func TestEndpointMissingTagWithProviderRestriction(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		fail        bool
	}{
		{"only excludes unidentified endpoint", `,"provider":{"only":["cheap"]}`, false},
		{"order without fallbacks excludes unidentified endpoint", `,"provider":{"order":["cheap"],"allow_fallbacks":false}`, false},
		{"order with fallbacks retains unidentified endpoint", `,"provider":{"order":["cheap"]}`, true},
		{"unrestricted unidentified endpoint remains invalid", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewRequestEstimator()
			var endpoints []PricedEndpoint
			require.NoError(t, common.UnmarshalJsonStr(`[{"tag":"cheap/base","pricing":{"prompt":"0.001","completion":"0.002"}},{"tag":"","status":-2,"pricing":{"prompt":"0.003","completion":"0.004"}}]`, &endpoints))
			e.Models["test/model"] = endpoints
			info := &relaycommon.RelayInfo{}
			info.SetEstimatePromptTokens(1000)
			cost, err := e.Estimate(info, []byte(`{"model":"test/model","max_tokens":10`+tc.extra+`}`), "chat/completions")
			if tc.fail {
				require.ErrorContains(t, err, "endpoint tag is missing")
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, 1.02, cost, 1e-9)
		})
	}
}

// A channel on the official OpenRouter API prices a routable model from the
// fresh upstream catalog without discovering it live, and Batch from its
// :batch variant; another channel does not read the catalog.
func TestEndpointReserveFromUpstreamCatalog(t *testing.T) {
	previous := pricingruntime.UpstreamCatalogTariffs.Load()
	t.Cleanup(func() { pricingruntime.UpstreamCatalogTariffs.Store(previous) })
	tariff := func(prompt, completion string) pricingdomain.NativeEndpointResponse {
		var pricing map[string]json.RawMessage
		require.NoError(t, common.UnmarshalJsonStr(`{"prompt":"`+prompt+`","completion":"`+completion+`"}`, &pricing))
		return pricingdomain.NativeEndpointResponse{Endpoints: []pricingdomain.NativePricedEndpoint{{Tag: "provider", MaxCompletionTokens: 100, Pricing: pricing}}, OutputModalities: []string{"text"}}
	}
	pricingruntime.UpstreamCatalogTariffs.Store(&pricingdomain.UpstreamCatalogTariffs{FetchedAt: time.Now(), Models: map[string]pricingdomain.NativeEndpointResponse{
		"test/model": tariff("0.001", "0.002"), "test/model:batch": tariff("0.0005", "0.001"),
	}})
	for _, tc := range []struct {
		name, operation, baseURL, body, wantErr string
		want                                    float64
	}{
		{name: "sync", operation: "chat/completions", baseURL: "https://openrouter.ai/api", want: 1.02},
		{name: "batch", operation: "batch_chat/completions", baseURL: "https://openrouter.ai/api", want: .51},
		{name: "other channel", operation: "chat/completions", baseURL: "https://proxy.example/api", wantErr: "OpenRouter endpoint discovery missing for test/model"},
		{name: "batch item with malformed models", operation: "batch_chat/completions", baseURL: "https://openrouter.ai/api", body: `{"model":"test/model","models":"test/model","max_tokens":10}`, wantErr: "invalid OpenRouter final request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewRequestEstimator()
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: tc.baseURL}
			info.SetEstimatePromptTokens(1000)
			cost, err := e.Estimate(info, []byte(cmp.Or(tc.body, `{"model":"test/model","max_tokens":10}`)), tc.operation)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-9)
			assert.Equal(t, pricingdomain.ReserveSourceCatalog, e.ReserveSource)
			assert.Empty(t, e.Models)
		})
	}
}

// A speech request is measured in the units a TTS endpoint prices per
// request: the characters and UTF-8 bytes of the input, the longer of two
// words a second and five characters a second, lengthened by a speed below 1
// and never shortened by a faster one, and the tokens of the input plus those
// of the instructions.
func TestSpeechUnits(t *testing.T) {
	const model = "acme/tts"
	for _, tc := range []struct {
		name, input, instructions  string
		speed                      *float64
		characters, bytes, seconds int64
		wantErr                    string
	}{
		{name: "characters outlast words", input: "Hello brave new world of speech", characters: 31, bytes: 31, seconds: 7},
		{name: "instructions add their tokens", input: "one two three four five", instructions: "calm", characters: 23, bytes: 23, seconds: 5},
		{name: "words outlast characters", input: "a b c d e f g h i j k l", characters: 23, bytes: 23, seconds: 6},
		{name: "cjk", input: "你好世界，今天天气很好", characters: 11, bytes: 33, seconds: 3},
		{name: "comma-joined words with one space", input: "one,two,three,four,five,six,seven,eight nine", characters: 44, bytes: 44, seconds: 9},
		{name: "thai with sentence spaces", input: "สวัสดีครับ ยินดีต้อนรับสู่ประเทศไทย", characters: 35, bytes: 103, seconds: 7},
		{name: "speed 4 does not shorten", input: "Hello brave new world of speech", speed: common.GetPointer(4.0), characters: 31, bytes: 31, seconds: 7},
		{name: "speed 0.5 lengthens", input: "Hello brave new world of speech", speed: common.GetPointer(0.5), characters: 31, bytes: 31, seconds: 14},
		{name: "speed 0.25 lengthens", input: "Hello brave new world of speech", speed: common.GetPointer(0.25), characters: 31, bytes: 31, seconds: 28},
		{name: "speed below the range", input: "hello", speed: common.GetPointer(0.2), wantErr: "OpenRouter speech speed must be between 0.25 and 4"},
		{name: "speed above the range", input: "hello", speed: common.GetPointer(4.5), wantErr: "OpenRouter speech speed must be between 0.25 and 4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := common.Marshal(map[string]any{"input": tc.input, "instructions": tc.instructions, "speed": tc.speed})
			require.NoError(t, err)
			units, err := speechUnits(body, model)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, map[pricingdomain.PriceDimension]int64{
				{Billable: pricingdomain.PriceBillableOutputSpeech, Unit: pricingdomain.PriceUnitCharacter}:  tc.characters,
				{Billable: pricingdomain.PriceBillableOutputSpeech, Unit: pricingdomain.PriceUnitUTF8Byte}:   tc.bytes,
				{Billable: pricingdomain.PriceBillableOutputAudio, Unit: pricingdomain.PriceUnitSecond}:      tc.seconds,
				{Billable: pricingdomain.PriceBillableInputText, Unit: pricingdomain.PriceUnitMillionTokens}: int64(tokenusage.CountTextToken(tc.input, model) + tokenusage.CountTextToken(tc.instructions, model)),
			}, units)
		})
	}
}

// Tariff maps every field of a live endpoint onto the domain tariff of the
// same name; a field added to PricedEndpoint must be mapped too.
func TestPricedEndpointTariffMapsEveryField(t *testing.T) {
	status := 1
	live := PricedEndpoint{
		Status: &status, ModelID: "acme/model", Tag: "provider", ContextLength: 1, MaxPromptTokens: 2, MaxCompletionTokens: 3,
		Quantization: "fp8", SupportedParameters: []string{"tools"}, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`)},
	}
	tariff := reflect.ValueOf(live.Tariff())
	source := reflect.ValueOf(live)
	for _, field := range reflect.VisibleFields(source.Type()) {
		require.False(t, source.FieldByIndex(field.Index).IsZero(), "set %s in the test endpoint", field.Name)
		mapped := tariff.FieldByName(field.Name)
		require.True(t, mapped.IsValid(), "NativePricedEndpoint has no field %s", field.Name)
		assert.Equal(t, source.FieldByIndex(field.Index).Interface(), mapped.Interface(), field.Name)
	}
	assert.Nil(t, live.Tariff().UnitPrices)
}

// Speech on the official API is priced from the catalog unit prices without a
// channel reserve; transcription still requires one.
func TestEndpointSpeechReserveFromCatalogUnitPrices(t *testing.T) {
	previous := pricingruntime.UpstreamCatalogTariffs.Load()
	t.Cleanup(func() { pricingruntime.UpstreamCatalogTariffs.Store(previous) })
	perCharacter := decimal.RequireFromString("0.00003")
	pricingruntime.UpstreamCatalogTariffs.Store(&pricingdomain.UpstreamCatalogTariffs{FetchedAt: time.Now(), Models: map[string]pricingdomain.NativeEndpointResponse{
		"acme/tts": {ID: "acme/tts", OutputModalities: []string{"speech"}, Endpoints: []pricingdomain.NativePricedEndpoint{{
			Tag: "deepgram", ModelID: "acme/tts", UnitPrices: []pricingdomain.EndpointUnitPrice{
				{Dimension: pricingdomain.PriceDimension{Billable: pricingdomain.PriceBillableOutputSpeech, Unit: pricingdomain.PriceUnitCharacter}, USD: &perCharacter},
			},
		}}},
	}})
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIAudio}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: "https://openrouter.ai/api"}
	e := NewRequestEstimator()
	cost, err := e.Estimate(info, []byte(`{"model":"acme/tts","input":"hello world"}`), "audio/speech")
	require.NoError(t, err)
	assert.InDelta(t, .00033, cost, 1e-12)
	assert.Equal(t, pricingdomain.ReserveSourceCatalog, e.ReserveSource)
	assert.Equal(t, ReserveNotApplied, e.ReserveUse)

	_, err = NewRequestEstimator().Estimate(info, []byte(`{"model":"acme/tts"}`), "audio/transcriptions")
	require.EqualError(t, err, "OpenRouter audio endpoint requires configured_price or an explicit approximate channel reserve (not a spending limit) and OpenAI Audio format")
}

// EstimateRequestReserve on an estimator without a bound request prices the
// admission reserve from the upstream catalog and never discovers live
// endpoints: a model outside the catalog is refused without a request, and a
// stale document is priced and logged without a request context.
func TestEstimateRequestReserveWithoutBoundRequest(t *testing.T) {
	previous := pricingruntime.UpstreamCatalogTariffs.Load()
	t.Cleanup(func() { pricingruntime.UpstreamCatalogTariffs.Store(previous) })
	previousPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"public-model":5}`))
	tariff := func(pricing string) pricingdomain.NativeEndpointResponse {
		var prices map[string]json.RawMessage
		require.NoError(t, common.UnmarshalJsonStr(pricing, &prices))
		return pricingdomain.NativeEndpointResponse{Endpoints: []pricingdomain.NativePricedEndpoint{{Tag: "provider", MaxCompletionTokens: 100, Pricing: prices}}, OutputModalities: []string{"text"}}
	}
	models := map[string]pricingdomain.NativeEndpointResponse{
		"test/model":  tariff(`{"prompt":"0.001","completion":"0.002"}`),
		"test/search": tariff(`{"prompt":"0.001","completion":"0.002","web_search":"0.01"}`),
	}
	for _, tc := range []struct {
		name, model string
		stale       bool
		settings    dto.ChannelSettings
		want        float64
		configured  bool
		source      pricingdomain.ReserveSource
		use         ReserveUse
		wantErr     string
	}{
		{name: "catalog", model: "test/model", want: 1.2, source: pricingdomain.ReserveSourceCatalog},
		{name: "add-on on a web_search endpoint", model: "test/search", settings: dto.ChannelSettings{OpenRouterModelReserveUSD: map[string]float64{"test/search": .05}},
			want: 1.25, source: pricingdomain.ReserveSourceCatalog, use: ReserveAddOn},
		{name: "configured price above the catalog", model: "test/model", settings: dto.ChannelSettings{OpenRouterReservationSource: map[string]dto.OpenRouterReservationSource{"test/model": dto.OpenRouterReservationConfiguredPrice}},
			want: 5, configured: true, source: pricingdomain.ReserveSourceConfiguredPrice, use: ReserveWholeHold},
		{name: "outside the catalog", model: "other/model", wantErr: "OpenRouter endpoint discovery missing for other/model"},
		{name: "stale document logged without a request", model: "test/model", stale: true, want: 1.2, source: pricingdomain.ReserveSourceCatalogStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetchedAt := time.Now()
			if tc.stale {
				fetchedAt = fetchedAt.Add(-pricingruntime.CatalogMaxAge() - time.Hour)
			}
			pricingruntime.UpstreamCatalogTariffs.Store(&pricingdomain.UpstreamCatalogTariffs{FetchedAt: fetchedAt, Models: models})
			e := NewRequestEstimator()
			info := &relaycommon.RelayInfo{OriginModelName: "public-model", RelayFormat: types.RelayFormatOpenAI}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: "https://openrouter.ai/api", ChannelSetting: tc.settings}
			info.SetEstimatePromptTokens(1000)

			cost, configured, err := EstimateRequestReserve(e, info, []byte(`{"model":"`+tc.model+`","messages":[{"role":"user","content":""}]}`), "chat/completions", nil)

			assert.Empty(t, e.Models)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				assert.ErrorIs(t, err, pricingdomain.ErrEndpointDiscoveryMissing)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-9)
			assert.Equal(t, tc.configured, configured != nil)
			assert.Equal(t, tc.source, e.ReserveSource)
			assert.Equal(t, tc.use, e.ReserveUse)
		})
	}
}
