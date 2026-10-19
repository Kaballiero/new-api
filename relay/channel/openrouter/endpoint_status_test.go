package openrouter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
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
		{"missing cap", "", "", "chat/completions", false},
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

func TestEndpointStatusRequestDiscovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/models/test/model/endpoints", r.URL.Path)
		gets++
		status := -2
		if gets > 1 {
			status = 0
		}
		_, err := fmt.Fprintf(w, `{"data":{"id":"test/model","endpoints":[{"tag":"provider","status":%d,"max_completion_tokens":100,"pricing":{"prompt":"0.001","completion":"0.002"}}]}}`, status)
		assert.NoError(t, err)
	}))
	defer server.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}
	info.SetEstimatePromptTokens(1000)
	e := NewRequestEstimator()
	for range 3 {
		require.NoError(t, e.Discover(c, info, &imageTestAdaptor{}, "test/model"))
		_, err := e.Estimate(info, []byte(`{"model":"test/model","max_tokens":10}`), "batch_chat/completions")
		require.NoError(t, err)
	}
	assert.Equal(t, 1, gets)
	assert.Equal(t, -2, *e.Models["test/model"][0].Status)
	next := NewRequestEstimator()
	require.NoError(t, next.Discover(c, info, &imageTestAdaptor{}, "test/model"))
	assert.Equal(t, 2, gets)
	assert.Equal(t, 0, *next.Models["test/model"][0].Status)
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
