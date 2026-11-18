package openrouter

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
			{"stt", "audio/transcriptions", types.RelayFormatOpenAIAudio, "transcription", `,"file":{"seconds":60}`, true, .25},
			{"chat audio music", "chat/completions", types.RelayFormatOpenAI, "audio", `,"modalities":["audio"],"stream":true`, true, .25},
			{"decisions", "decisions", types.RelayFormatSystemOne, "decisions", `,"state":{},"questions":{"q":{}}`, true, .25},
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

// A rerank request is measured in Cohere search units, one per query with up
// to 100 documents, a document longer than 500 tokens with the query split
// into chunks that count as documents, and in input tokens by the Voyage rule,
// the query tokens times the documents plus the document tokens. The tokens of
// a text are its UTF-8 bytes, a bound that holds for long single words too; a
// document object counts as its JSON text.
func TestRerankUnits(t *testing.T) {
	textOf := func(bytes int) string { return strings.Repeat("a", bytes) }
	documentsOf := func(count int, document any) []any {
		documents := make([]any, count)
		for i := range documents {
			documents[i] = document
		}
		return documents
	}
	for _, tc := range []struct {
		name             string
		query            string
		documents        []any
		searches, tokens int64
	}{
		{name: "100 documents of 499 tokens are one search", documents: documentsOf(100, textOf(499)), searches: 1, tokens: 49900},
		{name: "100 documents of 500 tokens are one search", documents: documentsOf(100, textOf(500)), searches: 1, tokens: 50000},
		{name: "a document of 501 tokens is two chunks", documents: documentsOf(50, textOf(501)), searches: 1, tokens: 25050},
		{name: "51 documents of 501 tokens are two searches", documents: documentsOf(51, textOf(501)), searches: 2, tokens: 25551},
		{name: "a document of 1500 tokens is three chunks", documents: documentsOf(34, textOf(1500)), searches: 2, tokens: 51000},
		{name: "a long single word counts every byte", documents: documentsOf(3, textOf(20000)), searches: 2, tokens: 60000},
		{name: "a multi-byte character counts its bytes", documents: documentsOf(100, strings.Repeat("字", 167)), searches: 2, tokens: 50100},
		{name: "the query shortens the chunks", query: textOf(100), documents: documentsOf(50, textOf(401)), searches: 1, tokens: 50*100 + 50*401},
		{name: "the query splits a document of 401 tokens", query: textOf(100), documents: documentsOf(51, textOf(401)), searches: 2, tokens: 51*100 + 51*401},
		{name: "a query of 500 tokens makes every document token a chunk", query: textOf(500), documents: documentsOf(1, textOf(101)), searches: 2, tokens: 500 + 101},
		{name: "101 documents are two searches", query: textOf(3), documents: documentsOf(101, textOf(2)), searches: 2, tokens: 101*3 + 101*2},
		{name: "a document object counts as its JSON text", documents: []any{map[string]any{"text": textOf(10)}, textOf(5)}, searches: 1, tokens: int64(len(`{"text":"aaaaaaaaaa"}`)) + 5},
		{name: "no documents is one search", query: textOf(3), searches: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := common.Marshal(map[string]any{"model": "cohere/rerank-v3.5", "query": tc.query, "documents": tc.documents})
			require.NoError(t, err)
			units, err := rerankUnits(body)
			require.NoError(t, err)
			assert.Equal(t, map[pricingdomain.PriceDimension]int64{
				{Billable: pricingdomain.PriceBillableSearch, Unit: pricingdomain.PriceUnitSearch}:           tc.searches,
				{Billable: pricingdomain.PriceBillableInputText, Unit: pricingdomain.PriceUnitMillionTokens}: tc.tokens,
			}, units)
		})
	}
	_, err := rerankUnits([]byte(`{"query":{}}`))
	require.EqualError(t, err, "invalid OpenRouter rerank request")
}

// A decisions request reserves the prompt tokens of its state and questions at
// the prompt price, a token per UTF-8 byte for every model, and one search per
// question, at most decisionSearchQuestionLimit, at the web_search price. The
// admin log records both quantities.
func TestEndpointDecisionReserve(t *testing.T) {
	questionsOf := func(count int) string {
		questions := make(map[string]any, count)
		for i := range count {
			questions[fmt.Sprintf("q%d", i)] = map[string]any{"type": "noul", "instructions": "Is the order late?"}
		}
		raw, err := common.Marshal(questions)
		require.NoError(t, err)
		return string(raw)
	}
	const state = `"Заказ 42 отправлен вчера"`
	for _, tc := range []struct {
		name, model, questions string
		wantSearches           int
	}{
		{name: "a token per byte", model: "google/gemini-decision-1", questions: questionsOf(3), wantSearches: 3},
		{name: "searches capped at 100 questions", model: "google/gemini-decision-1", questions: questionsOf(101), wantSearches: decisionSearchQuestionLimit},
		{name: "an OpenAI model counts bytes too", model: "openai/gpt-6-luna-decisions", questions: questionsOf(3), wantSearches: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var endpoint PricedEndpoint
			require.NoError(t, common.UnmarshalJsonStr(`{"tag":"provider","model_id":"`+tc.model+`","pricing":{"prompt":"0.000001","completion":"0","web_search":"0.01"}}`, &endpoint))
			e := NewRequestEstimator()
			e.Models[tc.model] = []PricedEndpoint{endpoint}
			e.OutputModalities[tc.model] = []string{"decisions"}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatSystemOne}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			cost, err := e.Estimate(info, []byte(`{"model":"`+tc.model+`","state":`+state+`,"questions":`+tc.questions+`}`), "decisions")
			require.NoError(t, err)
			wantPrompt := len(state) + len(tc.questions)
			assert.Equal(t, map[pricingdomain.ReserveQuantity]int{
				pricingdomain.ReserveQuantitySearches:     tc.wantSearches,
				pricingdomain.ReserveQuantityPromptTokens: wantPrompt,
			}, e.ReserveQuantities)
			assert.InDelta(t, float64(wantPrompt)*.000001+float64(tc.wantSearches)*.01, cost, 1e-9)
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

// Speech and rerank on the official API are priced from the catalog unit
// prices without a channel reserve, and a rerank model without catalog unit
// prices is refused without a reserve.
func TestEndpointReserveFromCatalogUnitPrices(t *testing.T) {
	previous := pricingruntime.UpstreamCatalogTariffs.Load()
	t.Cleanup(func() { pricingruntime.UpstreamCatalogTariffs.Store(previous) })
	perCharacter := decimal.RequireFromString("0.00003")
	perSearch := decimal.RequireFromString("0.002")
	pricingruntime.UpstreamCatalogTariffs.Store(&pricingdomain.UpstreamCatalogTariffs{FetchedAt: time.Now(), Models: map[string]pricingdomain.NativeEndpointResponse{
		"acme/tts": {ID: "acme/tts", OutputModalities: []string{"speech"}, Endpoints: []pricingdomain.NativePricedEndpoint{{
			Tag: "deepgram", ModelID: "acme/tts", UnitPrices: []pricingdomain.EndpointUnitPrice{
				{Dimension: pricingdomain.PriceDimension{Billable: pricingdomain.PriceBillableOutputSpeech, Unit: pricingdomain.PriceUnitCharacter}, USD: &perCharacter},
			},
		}}},
		"acme/rerank": {ID: "acme/rerank", OutputModalities: []string{"rerank"}, Endpoints: []pricingdomain.NativePricedEndpoint{{
			Tag: "cohere", ModelID: "acme/rerank", UnitPrices: []pricingdomain.EndpointUnitPrice{
				{Dimension: pricingdomain.PriceDimension{Billable: pricingdomain.PriceBillableSearch, Unit: pricingdomain.PriceUnitSearch}, USD: &perSearch},
			},
		}}},
	}})
	rerankInfo := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatRerank}
	rerankInfo.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: "https://openrouter.ai/api"}
	documents := strings.Repeat(`"hello",`, 100) + `"hello"`
	e := NewRequestEstimator()
	cost, err := e.Estimate(rerankInfo, []byte(`{"model":"acme/rerank","query":"hi","documents":[`+documents+`]}`), "rerank")
	require.NoError(t, err)
	assert.InDelta(t, .004, cost, 1e-12)
	assert.Equal(t, pricingdomain.ReserveSourceCatalog, e.ReserveSource)
	assert.Equal(t, ReserveNotApplied, e.ReserveUse)

	e = NewRequestEstimator()
	e.Models["other/rerank"] = []PricedEndpoint{{Tag: "cohere", ModelID: "other/rerank", Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0"`), "completion": json.RawMessage(`"0"`)}}}
	e.OutputModalities["other/rerank"] = []string{"rerank"}
	_, err = e.Estimate(rerankInfo, []byte(`{"model":"other/rerank","query":"hi","documents":["hello"]}`), "rerank")
	require.EqualError(t, err, "OpenRouter estimate unavailable: OpenRouter endpoint unit prices are unknown")

	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIAudio}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: "https://openrouter.ai/api"}
	e = NewRequestEstimator()
	cost, err = e.Estimate(info, []byte(`{"model":"acme/tts","input":"hello world"}`), "audio/speech")
	require.NoError(t, err)
	assert.InDelta(t, .00033, cost, 1e-12)
	assert.Equal(t, pricingdomain.ReserveSourceCatalog, e.ReserveSource)
	assert.Equal(t, ReserveNotApplied, e.ReserveUse)
}

// Transcription on the official API is priced from the catalog unit prices
// without a channel reserve: its seconds × the price of a second, the price of
// a request-dependent variant at its maximum, with the measured seconds as the
// reserve quantity audio_seconds. A model priced only by tokens, or a file whose duration is unreadable,
// measures no quantity it is priced by: a configured price or the channel
// reserve covers it, and without one it is refused, never reserved at $0.
func TestEndpointTranscriptionReserveFromCatalogUnitPrices(t *testing.T) {
	tokenusage.InitTokenEncoders()
	previous := pricingruntime.UpstreamCatalogTariffs.Load()
	t.Cleanup(func() { pricingruntime.UpstreamCatalogTariffs.Store(previous) })
	price := func(billable pricingdomain.PriceBillable, unit pricingdomain.PriceUnit, usd string) pricingdomain.EndpointUnitPrice {
		amount := decimal.RequireFromString(usd)
		return pricingdomain.EndpointUnitPrice{Dimension: pricingdomain.PriceDimension{Billable: billable, Unit: unit}, USD: &amount}
	}
	// transcription is a model whose endpoint has the pricing rates and the
	// unit prices of its catalog document.
	transcription := func(model, prompt string, prices ...pricingdomain.EndpointUnitPrice) pricingdomain.NativeEndpointResponse {
		pricing := map[string]json.RawMessage{"prompt": json.RawMessage(`"` + prompt + `"`), "completion": json.RawMessage(`"0"`)}
		return pricingdomain.NativeEndpointResponse{ID: model, OutputModalities: []string{"transcription"}, Endpoints: []pricingdomain.NativePricedEndpoint{{Tag: "provider", ModelID: model, Pricing: pricing, UnitPrices: prices}}}
	}
	pricingruntime.UpstreamCatalogTariffs.Store(&pricingdomain.UpstreamCatalogTariffs{FetchedAt: time.Now(), Models: map[string]pricingdomain.NativeEndpointResponse{
		// The catalog documents of whisper-1 and gpt-4o-transcribe, and the
		// unit price of the prompted universal-3-6-pro; prompt mirrors a unit
		// price.
		"openai/whisper-1": transcription("openai/whisper-1", "0.0001",
			price(pricingdomain.PriceBillableInputAudio, pricingdomain.PriceUnitMillionTokens, "0"),
			price("output_text", pricingdomain.PriceUnitMillionTokens, "0"),
			price(pricingdomain.PriceBillableInputAudio, pricingdomain.PriceUnitSecond, "0.0001")),
		"openai/gpt-4o-transcribe": transcription("openai/gpt-4o-transcribe", "0.0000025",
			price(pricingdomain.PriceBillableInputAudio, pricingdomain.PriceUnitSecond, "0"),
			price("output_text", pricingdomain.PriceUnitMillionTokens, "10"),
			price(pricingdomain.PriceBillableInputAudio, pricingdomain.PriceUnitMillionTokens, "2.5"),
			price(pricingdomain.PriceBillableInputText, pricingdomain.PriceUnitMillionTokens, "2.5")),
		"assemblyai/universal-3-6-pro": transcription("assemblyai/universal-3-6-pro", "0.0000972222",
			price(pricingdomain.PriceBillableInputAudio, pricingdomain.PriceUnitSecond, "0.0000972222")),
		// A zero price of a second with its token price only in the endpoint
		// pricing.
		"acme/stt-tokens": transcription("acme/stt-tokens", "0.0000025",
			price(pricingdomain.PriceBillableInputAudio, pricingdomain.PriceUnitSecond, "0")),
	}})
	for _, tc := range []struct {
		name, operation, body string
		channelReserveUSD     *float64
		configuredUSD         *float64
		want                  float64
		source                pricingdomain.ReserveSource
		use                   ReserveUse
		quantities            map[pricingdomain.ReserveQuantity]int
		wantErr               string
	}{
		{name: "whisper-1 per second", operation: "audio/transcriptions", body: `{"model":"openai/whisper-1","file":{"seconds":60}}`, want: .006, source: pricingdomain.ReserveSourceCatalog, quantities: map[pricingdomain.ReserveQuantity]int{pricingdomain.ReserveQuantityAudioSeconds: 60}},
		{name: "prompted variant at its maximum", operation: "audio/transcriptions", body: `{"model":"assemblyai/universal-3-6-pro","file":{"seconds":3600}}`, want: .34999992, source: pricingdomain.ReserveSourceCatalog, quantities: map[pricingdomain.ReserveQuantity]int{pricingdomain.ReserveQuantityAudioSeconds: 3600}},
		{name: "token-only model without a reserve", operation: "audio/transcriptions", body: `{"model":"openai/gpt-4o-transcribe","file":{"seconds":60}}`, wantErr: "OpenRouter estimate unavailable: OpenRouter unit quantity is unmeasured: output_text/million_tokens"},
		{name: "a token price outside the unit prices is refused, not $0", operation: "audio/transcriptions", body: `{"model":"acme/stt-tokens","file":{"seconds":60}}`, wantErr: "OpenRouter estimate unavailable: OpenRouter endpoint price prompt is not covered by its unit prices"},
		{name: "token-only model with a channel reserve", operation: "audio/transcriptions", body: `{"model":"openai/gpt-4o-transcribe","file":{"seconds":60}}`, channelReserveUSD: common.GetPointer(.5), want: .5, source: pricingdomain.ReserveSourceManual, use: ReserveWholeHold, quantities: map[pricingdomain.ReserveQuantity]int{pricingdomain.ReserveQuantityAudioSeconds: 60}},
		{name: "a channel reserve above the catalog price", operation: "audio/transcriptions", body: `{"model":"openai/whisper-1","file":{"seconds":60}}`, channelReserveUSD: common.GetPointer(.5), want: .5, source: pricingdomain.ReserveSourceManual, use: ReserveWholeHold, quantities: map[pricingdomain.ReserveQuantity]int{pricingdomain.ReserveQuantityAudioSeconds: 60}},
		{name: "unreadable duration with a channel reserve", operation: "audio/transcriptions", body: `{"model":"openai/whisper-1","file":{"filename":"speech.wav","size":18}}`, channelReserveUSD: common.GetPointer(.5), want: .5, source: pricingdomain.ReserveSourceManual, use: ReserveWholeHold},
		{name: "unreadable duration with a configured price", operation: "audio/transcriptions", body: `{"model":"openai/whisper-1","file":{"filename":"speech.wav","size":18}}`, configuredUSD: common.GetPointer(.25), want: .25, source: pricingdomain.ReserveSourceConfiguredPrice, use: ReserveWholeHold},
		{name: "unreadable duration without a cover", operation: "audio/transcriptions", body: `{"model":"openai/whisper-1","file":{"filename":"speech.wav","size":18}}`, wantErr: "OpenRouter transcription audio duration is unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var settings dto.ChannelSettings
			if tc.channelReserveUSD != nil {
				var request struct {
					Model string `json:"model"`
				}
				require.NoError(t, common.UnmarshalJsonStr(tc.body, &request))
				settings.OpenRouterModelReserveUSD = map[string]float64{request.Model: *tc.channelReserveUSD}
			}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIAudio}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: "https://openrouter.ai/api", ChannelSetting: settings}
			e := NewRequestEstimator()
			var cost float64
			var err error
			if tc.configuredUSD != nil {
				cost, err = e.EstimateWithLocalPrice(info, []byte(tc.body), tc.operation, *tc.configuredUSD)
			} else {
				cost, err = e.Estimate(info, []byte(tc.body), tc.operation)
			}
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-12)
			assert.Equal(t, tc.source, e.ReserveSource)
			assert.Equal(t, tc.use, e.ReserveUse)
			assert.Equal(t, tc.quantities, e.ReserveQuantities)
		})
	}
}

// The reservation payload of a transcription carries the seconds of its file
// from the audio headers, rounded up and saturated to transcriptionMaxSeconds,
// so headers claiming a longer duration reserve no more, and never below the longest duration any reader yields: the reader of the extension
// (.mpga is MP3), the reader its magic bytes name, and for WAV the bytes after
// the data chunk header. A duration that cannot be read leaves the seconds
// out.
func TestTranscriptionReservationPayloadSeconds(t *testing.T) {
	// wav is a PCM WAV file of sampleRate 8-bit mono samples a second whose
	// data chunk header claims claimedBytes and holds dataBytes.
	wav := func(sampleRate, claimedBytes, dataBytes uint32) []byte {
		header := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x00\x08\x00data\x00\x00\x00\x00")
		binary.LittleEndian.PutUint32(header[4:], 36+claimedBytes)
		binary.LittleEndian.PutUint32(header[24:], sampleRate)
		binary.LittleEndian.PutUint32(header[28:], sampleRate)
		binary.LittleEndian.PutUint32(header[40:], claimedBytes)
		return append(header, make([]byte, dataBytes)...)
	}
	// mp3 is frames MPEG-1 Layer III frames of 128 kbit/s at 44.1 kHz, 417
	// bytes and 1152 samples each.
	mp3 := func(frames int) []byte {
		frame := append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 413)...)
		return bytes.Repeat(frame, frames)
	}
	for _, tc := range []struct {
		name, filename string
		audio          []byte
		seconds        *int64
	}{
		{name: "duration from the headers, rounded up", filename: "speech.wav", audio: wav(8000, 12000, 12000), seconds: common.GetPointer[int64](2)},
		{name: "upper-case extension", filename: "SPEECH.WAV", audio: wav(8000, 8000, 8000), seconds: common.GetPointer[int64](1)},
		{name: "a huge claimed duration saturates", filename: "speech.wav", audio: wav(8000, 0x7ffffff0, 16000), seconds: common.GetPointer[int64](transcriptionMaxSeconds)},
		{name: "a low sample rate is measured as claimed", filename: "speech.wav", audio: wav(1, 2000, 2000), seconds: common.GetPointer[int64](2000)},
		{name: "mpga is read as MP3", filename: "speech.mpga", audio: mp3(40), seconds: common.GetPointer[int64](2)},
		{name: "a file without an extension is sniffed", filename: "speech", audio: wav(8000, 12000, 12000), seconds: common.GetPointer[int64](2)},
		{name: "a header claiming a shorter duration is reserved by its data", filename: "speech.wav", audio: wav(8000, 4000, 16000), seconds: common.GetPointer[int64](2)},
		{name: "the extension reader cannot shorten the magic-byte reader", filename: "speech.mp3", audio: append(wav(8000, 834, 0), append(mp3(2), make([]byte, 16000)...)...), seconds: common.GetPointer[int64](3)},
		{name: "a mislabeled file is sniffed", filename: "speech.mp3", audio: wav(8000, 12000, 12000), seconds: common.GetPointer[int64](2)},
		{name: "unreadable headers leave the seconds out", filename: "speech.wav", audio: []byte("RIFF audio fixture")},
		{name: "an unknown format leaves the seconds out", filename: "speech", audio: []byte("not audio at all")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", "openai/whisper-1"))
			file, err := writer.CreateFormFile("file", tc.filename)
			require.NoError(t, err)
			_, err = file.Write(tc.audio)
			require.NoError(t, err)
			require.NoError(t, writer.Close())

			payload, err := transcriptionReservationPayload(context.Background(), writer.FormDataContentType(), body.Bytes())
			require.NoError(t, err)
			descriptor := map[string]any{"filename": tc.filename, "size": len(tc.audio)}
			if tc.seconds != nil {
				descriptor["seconds"] = *tc.seconds
			}
			want, err := common.Marshal(map[string]any{"model": "openai/whisper-1", "file": descriptor})
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(payload))
		})
	}
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
		// A Perplexity Sonar model searches on its own on every request.
		"perplexity/sonar-pro": tariff(`{"prompt":"0.000003","completion":"0.000015","web_search":"0.005"}`),
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
		{name: "floor below the estimate on a web_search endpoint", model: "test/search", settings: dto.ChannelSettings{OpenRouterModelReserveUSD: map[string]float64{"test/search": .05}},
			want: 1.21, source: pricingdomain.ReserveSourceCatalog, use: ReserveFloor},
		{name: "floor above the estimate is the manual reserve", model: "test/model", settings: dto.ChannelSettings{OpenRouterModelReserveUSD: map[string]float64{"test/model": 3}},
			want: 3, source: pricingdomain.ReserveSourceManual, use: ReserveFloor},
		{name: "sonar model prices one unrequested search", model: "perplexity/sonar-pro", want: .0095, source: pricingdomain.ReserveSourceCatalog},
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
