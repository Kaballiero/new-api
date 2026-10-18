package openai

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errorStreamWriter struct {
	gin.ResponseWriter
}

func setStreamTestTimeout(t *testing.T) {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
}

func (w errorStreamWriter) Write([]byte) (int, error) {
	return 0, errors.New("client write failed")
}

func (w errorStreamWriter) WriteString(string) (int, error) {
	return 0, errors.New("client write failed")
}

const openRouterEmbeddedError = `{"choices":[],"error":{"code":502,"message":"Upstream error from Nvidia: Service temporarily overloaded","metadata":{"error_type":"provider_unavailable"}}}`

func newEmbeddedErrorTestRequest(body string, statusCode int) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, recorder, &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		RelayFormat: types.RelayFormatOpenAI,
	}
}

func assertEmbeddedOpenAIError(t *testing.T, relayError *types.NewAPIError, recorder *httptest.ResponseRecorder, statusCode int) {
	t.Helper()
	require.NotNil(t, relayError)
	require.Equal(t, statusCode, relayError.StatusCode)
	require.Equal(t, "upstream_error", relayError.RelayError.(types.OpenAIError).Type)
	require.Contains(t, relayError.RelayError.(types.OpenAIError).Message, "Upstream error from Nvidia: Service temporarily overloaded")
	require.Equal(t, float64(502), relayError.RelayError.(types.OpenAIError).Code)
	require.JSONEq(t, `{"error_type":"provider_unavailable"}`, string(relayError.Metadata))
	require.Empty(t, recorder.Body.String())
}

func TestOpenAIHandlersSurfaceEmbeddedOpenRouterErrors(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	tests := []struct {
		name   string
		handle func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{name: "chat", handle: OpenaiHandler},
		{name: "image", handle: OpenaiImageHandler},
		{name: "image JSON as stream", handle: openaiImageJSONAsStreamHandler},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newEmbeddedErrorTestRequest(openRouterEmbeddedError, http.StatusOK)
			usage, relayError := tt.handle(c, info, resp)

			require.Nil(t, usage)
			assertEmbeddedOpenAIError(t, relayError, recorder, http.StatusBadGateway)
		})
	}
}

func TestOpenAIEmbeddedErrorRecognition(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		hasError bool
	}{
		{name: "message", body: `{"error":{"message":"unavailable"}}`, hasError: true},
		{name: "code", body: `{"error":{"code":123}}`, hasError: true},
		{name: "type", body: `{"error":{"type":"server_error"}}`, hasError: true},
		{name: "empty object", body: `{"error":{}}`},
		{name: "null", body: `{"error":null}`},
		{name: "missing", body: `{}`},
		{name: "empty string code", body: `{"error":{"code":""}}`},
		{name: "numeric zero code", body: `{"error":{"code":0}}`, hasError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response dto.SimpleResponse
			require.NoError(t, common.Unmarshal([]byte(tt.body), &response))
			assert.Equal(t, tt.hasError, hasOpenAIErrorContent(response.GetOpenAIError()))
		})
	}
}

func TestOpenAIEmbeddedErrorPreservesFailingStatusAndSuccessfulOutput(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	t.Run("failing upstream status", func(t *testing.T) {
		c, recorder, resp, info := newEmbeddedErrorTestRequest(`{"error":{"message":"unavailable"}}`, http.StatusServiceUnavailable)
		usage, relayError := OpenaiHandler(c, info, resp)

		require.Nil(t, usage)
		require.NotNil(t, relayError)
		require.Equal(t, http.StatusServiceUnavailable, relayError.StatusCode)
		require.Empty(t, recorder.Body.String())
	})

	t.Run("valid image response", func(t *testing.T) {
		body := `{"created":1710000000,"data":[{"url":"https://example.com/image"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`
		c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
		usage, relayError := OpenaiImageHandler(c, info, resp)

		require.Nil(t, relayError)
		require.NotNil(t, usage)
		require.Equal(t, body, recorder.Body.String())
	})
}

func TestOaiStreamHandlerSurfacesEmbeddedErrors(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	setStreamTestTimeout(t)

	t.Run("content and usage before error are retained", func(t *testing.T) {
		body := strings.Join([]string{
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
			``,
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`,
			``,
			`data: {"error":{"message":"upstream unavailable","code":"provider_unavailable"}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")
		c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
		info.RelayMode = relayconstant.RelayModeChatCompletions
		resp.Header.Set("Content-Type", "text/event-stream")

		usage, relayError := OaiStreamHandler(c, info, resp)

		require.Nil(t, relayError)
		require.Equal(t, 5, usage.TotalTokens)
		require.True(t, info.StreamStatus.HasErrors())
		require.Equal(t, 1, info.StreamStatus.TotalErrorCount())
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"content":"hello"`))
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"error"`))
		require.NotContains(t, recorder.Body.String(), "[DONE]")
		terminalError := strings.Index(recorder.Body.String(), `data: {"error":`)
		require.NotEqual(t, -1, terminalError)
		require.NotContains(t, recorder.Body.String()[terminalError:], `"content":"hello"`)
		require.NotContains(t, recorder.Body.String()[terminalError:], `"usage"`)
	})

	t.Run("latest usage before later chunks is retained", func(t *testing.T) {
		body := strings.Join([]string{
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[],"usage":{"prompt_tokens":200,"completion_tokens":300,"total_tokens":500}}`,
			``,
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"first"}}]}`,
			``,
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"second"}}]}`,
			``,
			`data: {"error":{"message":"upstream unavailable"}}`,
			``,
		}, "\n")
		c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
		info.RelayMode = relayconstant.RelayModeChatCompletions
		resp.Header.Set("Content-Type", "text/event-stream")

		usage, relayError := OaiStreamHandler(c, info, resp)

		require.Nil(t, relayError)
		require.Equal(t, 500, usage.TotalTokens)
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"content":"first"`))
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"content":"second"`))
		terminalError := strings.Index(recorder.Body.String(), `data: {"error":`)
		require.NotEqual(t, -1, terminalError)
		require.NotContains(t, recorder.Body.String()[terminalError:], `"content"`)
		require.NotContains(t, recorder.Body.String()[terminalError:], `"usage"`)
	})

	t.Run("terminal diagnostics are masked", func(t *testing.T) {
		body := "data: {\"error\":{\"message\":\"upstream api_key:credential\"}}\n"
		c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
		info.RelayMode = relayconstant.RelayModeChatCompletions
		resp.Header.Set("Content-Type", "text/event-stream")

		usage, relayError := OaiStreamHandler(c, info, resp)

		require.Nil(t, relayError)
		require.NotNil(t, usage)
		require.Equal(t, 1, info.StreamStatus.TotalErrorCount())
		require.NotContains(t, recorder.Body.String(), "credential")
		require.Len(t, info.StreamStatus.Errors, 1)
		require.NotContains(t, info.StreamStatus.Errors[0].Message, "credential")
		require.Contains(t, recorder.Body.String(), "***")
		require.Contains(t, info.StreamStatus.Errors[0].Message, "***")
	})

	t.Run("terminal error uses each client envelope", func(t *testing.T) {
		tests := []struct {
			name        string
			relayFormat types.RelayFormat
			contains    []string
		}{
			{name: "OpenAI", relayFormat: types.RelayFormatOpenAI, contains: []string{`data: {"error":`, `"upstream_error"`}},
			{name: "Claude", relayFormat: types.RelayFormatClaude, contains: []string{"event: error", `data: {"type":"error","error":`}},
			{name: "Gemini", relayFormat: types.RelayFormatGemini, contains: []string{`data: {"error":`}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				c, recorder, resp, info := newEmbeddedErrorTestRequest("data: {\"error\":{\"message\":\"upstream unavailable\",\"code\":\"provider_unavailable\"}}\n", http.StatusOK)
				info.RelayMode = relayconstant.RelayModeChatCompletions
				info.RelayFormat = tt.relayFormat
				info.ChannelSetting.ForceFormat = true
				info.ChannelSetting.ThinkingToContent = true
				resp.Header.Set("Content-Type", "text/event-stream")

				usage, relayError := OaiStreamHandler(c, info, resp)

				require.Nil(t, relayError)
				require.NotNil(t, usage)
				require.True(t, info.StreamStatus.HasErrors())
				require.Equal(t, 1, info.StreamStatus.TotalErrorCount())
				for _, expected := range tt.contains {
					require.Contains(t, recorder.Body.String(), expected)
				}
				require.NotContains(t, recorder.Body.String(), "[DONE]")
			})
		}
	})
}

func TestOaiStreamHandlerLeavesNonErrorsAndMalformedPayloadsUnchanged(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	setStreamTestTimeout(t)

	tests := []struct {
		name string
		data string
	}{
		{name: "top level message", data: `{"message":"not an error","choices":[]}`},
		{name: "empty error", data: `{"error":{},"choices":[]}`},
		{name: "null error", data: `{"error":null,"choices":[]}`},
		{name: "malformed", data: `{"choices":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := "data: " + tt.data + "\n\ndata: [DONE]\n"
			c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
			info.RelayMode = relayconstant.RelayModeChatCompletions
			resp.Header.Set("Content-Type", "text/event-stream")

			usage, relayError := OaiStreamHandler(c, info, resp)

			if tt.name == "malformed" {
				require.NotNil(t, relayError)
				require.Nil(t, usage)
				require.True(t, info.StreamStatus.HasErrors())
				require.NotContains(t, recorder.Body.String(), tt.data)
			} else {
				require.Nil(t, relayError)
				require.NotNil(t, usage)
				require.False(t, info.StreamStatus.HasErrors())
				require.Contains(t, recorder.Body.String(), tt.data)
				require.Contains(t, recorder.Body.String(), "[DONE]")
			}
		})
	}
}

func TestOaiStreamHandlerRecordsErrorEventWriteFailure(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	setStreamTestTimeout(t)

	c, _, resp, info := newEmbeddedErrorTestRequest("data: {\"error\":{\"message\":\"upstream unavailable\"}}\n", http.StatusOK)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	resp.Header.Set("Content-Type", "text/event-stream")
	c.Writer = errorStreamWriter{ResponseWriter: c.Writer}

	usage, relayError := OaiStreamHandler(c, info, resp)

	require.Nil(t, relayError)
	require.NotNil(t, usage)
	require.True(t, info.StreamStatus.HasErrors())
	require.GreaterOrEqual(t, info.StreamStatus.TotalErrorCount(), 2)
}

func TestOpenRouterCostGateBeforeClientWrite(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
		cost       float64
	}{
		{"cost only positive", `{"choices":[{"message":{"content":"ok"}}],"usage":{"cost":0.03}}`, true, .03},
		{"cost explicit zero", `{"choices":[{"message":{"content":"ok"}}],"usage":{"cost":0}}`, true, 0},
		{"missing cost", `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1}}`, false, 0},
		{"null cost", `{"choices":[{"message":{"content":"ok"}}],"usage":{"cost":null}}`, false, 0},
		{"string cost", `{"choices":[{"message":{"content":"ok"}}],"usage":{"cost":"0"}}`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, resp, info := newEmbeddedErrorTestRequest(tc.body, http.StatusOK)
			info.ChannelType = constant.ChannelTypeOpenRouter
			info.OpenRouterCost = &relaycommon.OpenRouterCostState{}
			info.SetEstimatePromptTokens(12)
			usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
			if !tc.valid {
				require.NotNil(t, apiErr)
				assert.Empty(t, recorder.Body.String())
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, tc.cost, usage.(*dto.Usage).Cost)
			assert.Contains(t, recorder.Body.String(), "ok")
		})
	}
}

func TestOpenRouterStreamRequiresTerminalCost(t *testing.T) {
	setStreamTestTimeout(t)
	for _, tc := range []struct {
		name, frames string
		valid        bool
		cost         float64
	}{
		{"final cost zero replaces provisional", "data: {\"choices\":[],\"usage\":{\"cost\":0.1}}\n\ndata: {\"choices\":[],\"usage\":{\"cost\":0}}\n\ndata: [DONE]\n\n", true, 0},
		{"null final invalidates provisional", "data: {\"choices\":[],\"usage\":{\"cost\":0.1}}\n\ndata: {\"choices\":[],\"usage\":{\"cost\":null}}\n\ndata: [DONE]\n\n", false, 0},
		{"disconnect without terminal", "data: {\"choices\":[],\"usage\":{\"cost\":0.1}}\n\n", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, resp, info := newEmbeddedErrorTestRequest(tc.frames, http.StatusOK)
			resp.Header.Set("Content-Type", "text/event-stream")
			info.ChannelType = constant.ChannelTypeOpenRouter
			info.IsStream = true
			info.OpenRouterCost = &relaycommon.OpenRouterCostState{}
			info.DisablePing = true
			usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
			if !tc.valid {
				require.NotNil(t, apiErr)
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, tc.cost, usage.(*dto.Usage).Cost)
		})
	}
}

func TestOpenRouterResponsesBillableTerminals(t *testing.T) {
	setStreamTestTimeout(t)
	for _, tc := range []struct {
		name, event, status, cost string
		valid                     bool
		want                      float64
	}{
		{"capped incomplete", "response.incomplete", "incomplete", `0.1`, true, .1},
		{"capped incomplete zero", "response.incomplete", "incomplete", `0`, true, 0},
		{"done without DONE marker", "response.done", "completed", `0.2`, true, .2},
		{"missing incomplete cost", "response.incomplete", "incomplete", ``, false, 0},
		{"null incomplete cost", "response.incomplete", "incomplete", `null`, false, 0},
		{"string incomplete cost", "response.incomplete", "incomplete", `"0"`, false, 0},
		{"failed event with cost", "response.failed", "failed", `0.1`, false, 0},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				costField := ""
				if tc.cost != "" {
					costField = `,"cost":` + tc.cost
				}
				response := fmt.Sprintf(`{"id":"resp_cost","object":"response","status":%q,"incomplete_details":{"reason":"max_output_tokens"},"output":[],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18%s}}`, tc.status, costField)
				body := response
				if stream {
					body = "data: " + fmt.Sprintf(`{"type":%q,"response":%s}`, tc.event, response) + "\n\n"
				}
				c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
				info.ChannelType = constant.ChannelTypeOpenRouter
				info.RelayMode = relayconstant.RelayModeResponses
				info.RelayFormat = types.RelayFormatOpenAIResponses
				info.OpenRouterCost = &relaycommon.OpenRouterCostState{}
				info.IsStream = stream
				info.DisablePing = true
				if stream {
					resp.Header.Set("Content-Type", "text/event-stream")
				}
				usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
				if !tc.valid {
					require.NotNil(t, apiErr)
					assert.False(t, info.OpenRouterCost.Final)
					if !stream {
						assert.Empty(t, recorder.Body.String())
					}
					return
				}
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				actual := usage.(*dto.Usage)
				assert.Equal(t, tc.want, actual.Cost)
				assert.Equal(t, 11, actual.PromptTokens)
				assert.Equal(t, 7, actual.CompletionTokens)
				assert.Equal(t, 18, actual.TotalTokens)
				assert.True(t, info.OpenRouterCost.Final)
				assert.Contains(t, recorder.Body.String(), tc.status)
			})
		}
	}
}

type sonarAdmissionTransport struct {
	metadata string
	response string
	gets     int
	posts    int
	body     []byte
}

func (s *sonarAdmissionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := s.metadata
	switch request.Method {
	case http.MethodGet:
		s.gets++
	case http.MethodPost:
		s.posts++
		var err error
		s.body, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"cost":0.47545,"cost_details":{"upstream_inference_cost":0.001032}}}`
		if s.response != "" {
			body = s.response
		}
	default:
		return nil, errors.New("unexpected request method")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

type sonarAdmissionBilling struct {
	reserved int
	err      error
}

func (s *sonarAdmissionBilling) Reserve(quota int) error {
	if s.err != nil {
		return s.err
	}
	s.reserved = quota
	return nil
}
func (s *sonarAdmissionBilling) GetPreConsumedQuota() int { return s.reserved }
func (s *sonarAdmissionBilling) Settle(int) error         { return nil }
func (s *sonarAdmissionBilling) Refund(*gin.Context)      {}
func (s *sonarAdmissionBilling) NeedsRefund() bool        { return s.reserved > 0 }

func TestOpenRouterSonarApproximateReserveAdmission(t *testing.T) {
	const model = "perplexity/sonar-deep-research"
	for _, tc := range []struct {
		name, model, extra, endpoint, rateKey string
		rate                                  any
		reserve                               *float64
		insufficient, knownAboveReserve       bool
		general                               bool
		policyModel                           string
		endpointModel                         string
		missingCost                           bool
		nonOpenRouter                         bool
		operation                             string
		wantError                             string
	}{
		{name: "general Sonar overrides legacy", general: true, reserve: common.GetPointer(2.0)},
		{name: "general missing terminal cost remains unknown", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), missingCost: true},
		{name: "general non OpenRouter rejected", model: "other/reasoner", general: true, nonOpenRouter: true, reserve: common.GetPointer(2.0), wantError: "one nonstream text chat response"},
		{name: "general batch rejected", model: "other/reasoner", general: true, operation: "batch_chat/completions", reserve: common.GetPointer(2.0), wantError: "one nonstream text chat response"},
		{name: "general Responses rejected", model: "other/reasoner", general: true, operation: "responses", reserve: common.GetPointer(2.0), wantError: "one nonstream text chat response"},

		{name: "general second model", model: "other/reasoner", endpoint: "other-provider", general: true, reserve: common.GetPointer(2.0)},
		{name: "general exact ID does not match alias", model: "other/reasoner-alias", policyModel: "other/reasoner", general: true, reserve: common.GetPointer(2.0), wantError: "unsupported billable dimension internal_reasoning"},
		{name: "general channel absent remains closed", model: "other/reasoner", general: true, wantError: "unsupported billable dimension internal_reasoning"},
		{name: "general invalid reserve", model: "other/reasoner", general: true, reserve: common.GetPointer(0.0), wantError: "invalid OpenRouter"},
		{name: "general huge reserve", model: "other/reasoner", general: true, reserve: common.GetPointer(1e100), wantError: "quota"},
		{name: "general insufficient reserve", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), insufficient: true, wantError: "insufficient test quota"},
		{name: "general known subtotal above minimum", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "completion", rate: "1", knownAboveReserve: true},
		{name: "general wrong endpoint model rejected", model: "other/reasoner", endpointModel: "other/different", general: true, reserve: common.GetPointer(2.0), wantError: "endpoint model does not match"},
		{name: "general missing prompt price", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "prompt", rate: nil, wantError: "invalid OpenRouter endpoint price"},
		{name: "general missing completion price", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "completion", rate: nil, wantError: "invalid OpenRouter endpoint price"},

		{name: "general missing tariff", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "internal_reasoning", rate: nil, wantError: "invalid OpenRouter endpoint price"},
		{name: "general unknown dimension", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "research_step", rate: "1", wantError: "unsupported billable dimension research_step"},
		{name: "general stream rejected", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), extra: `,"stream":true`, wantError: "one nonstream text chat response"},
		{name: "disabled by default", wantError: "research reserve policy is disabled"},
		{name: "zero reasoning rate cannot enable policy", rateKey: "internal_reasoning", rate: "0", wantError: "research reserve policy is disabled"},
		{name: "enabled channel hold and full total cost", reserve: common.GetPointer(2.0)},
		{name: "known subtotal exceeds approximate hold", reserve: common.GetPointer(2.0), rateKey: "completion", rate: "1", knownAboveReserve: true},
		{name: "client cannot enable policy", extra: `,"openrouter_sonar_research_reserve_usd":2`, wantError: "research reserve policy is disabled"},
		{name: "other model remains closed", model: "other/reasoner", reserve: common.GetPointer(2.0), wantError: "unsupported billable dimension internal_reasoning"},
		{name: "another provider remains closed", endpoint: "other-provider", reserve: common.GetPointer(2.0), wantError: "requires the Perplexity endpoint"},
		{name: "unknown charge remains closed", reserve: common.GetPointer(2.0), rateKey: "research_step", rate: "0.01", wantError: "unsupported billable dimension research_step"},
		{name: "null reasoning rate", reserve: common.GetPointer(2.0), rateKey: "internal_reasoning", rate: nil, wantError: "invalid OpenRouter endpoint price"},
		{name: "negative search rate", reserve: common.GetPointer(2.0), rateKey: "web_search", rate: "-1", wantError: "invalid OpenRouter endpoint price"},
		{name: "zero reserve rejected", reserve: common.GetPointer(0.0), wantError: "invalid OpenRouter approximate reserve"},
		{name: "negative reserve rejected", reserve: common.GetPointer(-2.0), wantError: "invalid OpenRouter approximate reserve"},
		{name: "nan reserve rejected", reserve: common.GetPointer(math.NaN()), wantError: "invalid OpenRouter approximate reserve"},
		{name: "infinite reserve rejected", reserve: common.GetPointer(math.Inf(1)), wantError: "invalid OpenRouter approximate reserve"},
		{name: "oversized reserve rejected", reserve: common.GetPointer(1e100), wantError: "quota"},
		{name: "insufficient reserve stops before paid post", reserve: common.GetPointer(2.0), insufficient: true, wantError: "insufficient test quota"},
		{name: "stream rejected", reserve: common.GetPointer(2.0), extra: `,"stream":true`, wantError: "one nonstream text chat response"},
		{name: "multiple outputs rejected", reserve: common.GetPointer(2.0), extra: `,"n":2`, wantError: "one nonstream text chat response"},
		{name: "fallback models rejected", reserve: common.GetPointer(2.0), extra: `,"models":["other/reasoner"]`, wantError: "one nonstream text chat response"},
		{name: "tools rejected", reserve: common.GetPointer(2.0), extra: `,"tools":[{"type":"function","function":{"name":"f"}}]`, wantError: "one nonstream text chat response"},
		{name: "hosted search rejected", reserve: common.GetPointer(2.0), extra: `,"web_search_options":{}`, wantError: "hosted-search quantity is unpriced"},
		{name: "preset rejected", reserve: common.GetPointer(2.0), extra: `,"preset":"some-preset"`, wantError: "unpriced execution dimensions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestedModel := tc.model
			if requestedModel == "" {
				requestedModel = model
			}
			tag := tc.endpoint
			if tag == "" {
				tag = "perplexity"
			}
			prices := map[string]any{"prompt": "0.000002", "completion": "0.000008", "internal_reasoning": "0.000003", "web_search": "0.005"}
			if tc.rateKey != "" {
				prices[tc.rateKey] = tc.rate
			}
			// Discovery includes fallback metadata; policy must reject the final
			// models[] before reservation rather than merely checking its primary.
			metadata, err := common.Marshal(map[string]any{"data": map[string]any{"endpoints": []any{map[string]any{"tag": tag, "model_id": tc.endpointModel, "context_length": 128000, "max_completion_tokens": 115200, "pricing": prices}}}})
			require.NoError(t, err)
			transport := &sonarAdmissionTransport{metadata: string(metadata)}
			if tc.missingCost {
				transport.response = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`
			}
			client, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
			require.NoError(t, err)
			oldTransport := client.Transport
			client.Transport = transport
			t.Cleanup(func() { client.Transport = oldTransport })
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Reply OK."}],"max_tokens":128%s}`, requestedModel, tc.extra)
			c, recorder, _, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
			info.ChannelType = constant.ChannelTypeOpenRouter
			info.ChannelBaseUrl = "https://openrouter.test/api"
			info.RequestURLPath = "/v1/chat/completions"
			info.RelayMode = relayconstant.RelayModeChatCompletions
			info.ChannelSetting.OpenRouterSonarResearchReserveUSD = tc.reserve
			if tc.general {
				info.ChannelSetting.OpenRouterSonarResearchReserveUSD = nil
				if tc.reserve != nil {
					policyModel := tc.policyModel
					if policyModel == "" {
						policyModel = requestedModel
					}
					info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{policyModel: *tc.reserve}
					if requestedModel == model {
						info.ChannelSetting.OpenRouterSonarResearchReserveUSD = common.GetPointer(1.0)
					}
				}
			}

			info.PriceData.BillingGroupRatio = 1.5
			billing := &sonarAdmissionBilling{}
			if tc.insufficient {
				billing.err = errors.New("insufficient test quota")
			}
			info.Billing = billing
			adaptor := &Adaptor{}
			if tc.nonOpenRouter || tc.operation != "" {
				operation := tc.operation
				if operation == "" {
					operation = "chat/completions"
				}
				if tc.nonOpenRouter {
					info.ChannelType = constant.ChannelTypeOpenAI
				}
				_, err := openrouter.NewRequestEstimator().Estimate(info, []byte(body), operation)
				require.ErrorContains(t, err, tc.wantError)
				assert.Zero(t, transport.posts)
				return
			}
			info.OriginModelName = "public-alias"
			info.UpstreamModelName = requestedModel
			response, err := adaptor.DoRequest(c, info, strings.NewReader(body))
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				var apiErr *types.NewAPIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				assert.Equal(t, types.ErrorCodeModelPriceError, apiErr.GetErrorCode())
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.Equal(t, 0, transport.posts)
				if info.OpenRouterCost != nil {
					assert.False(t, info.OpenRouterCost.Sent)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, transport.gets)
			assert.Equal(t, 1, transport.posts)
			assert.JSONEq(t, body, string(transport.body), "pricing and paid transport must inspect the same final body")
			require.NotNil(t, info.OpenRouterCost)
			assert.True(t, info.OpenRouterCost.Sent)
			require.NotNil(t, info.OpenRouterCost.ApproximateReserveUSD)
			assert.Equal(t, 2.0, *info.OpenRouterCost.ApproximateReserveUSD)
			assert.Equal(t, requestedModel, info.OpenRouterCost.ApproximateReserveModel)
			if tc.knownAboveReserve {
				assert.Greater(t, info.OpenRouterCost.Basis.EstimatedUSD, 128.0)
				assert.Greater(t, billing.reserved, 1500000)
			} else {
				assert.Equal(t, 2.0, info.OpenRouterCost.Basis.EstimatedUSD)
				assert.Equal(t, 1500000, billing.reserved)
			}
			assert.Equal(t, billing.reserved, info.FinalPreConsumedQuota)
			assert.True(t, info.ForcePreConsume)
			// Changes after admission cannot reprice the authoritative total.
			info.PriceData.BillingGroupRatio = 9
			*tc.reserve = 7
			if tc.general {
				info.ChannelSetting.OpenRouterModelReserveUSD[requestedModel] = 7
			}
			usage, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
			if tc.missingCost {
				require.NotNil(t, apiErr)
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.False(t, info.OpenRouterCost.Final)
				assert.False(t, info.OpenRouterCost.Valid)
				assert.Empty(t, recorder.Body.String())
				return
			}
			require.Nil(t, apiErr)
			assert.Equal(t, .47545, usage.(*dto.Usage).Cost)
			quota, clamp, err := info.OpenRouterCost.Basis.Quota(info.OpenRouterCost.CostUSD)
			require.NoError(t, err)
			require.Nil(t, clamp)
			assert.Equal(t, 356588, quota, "total usage.cost, not upstream_inference_cost or local surcharges")
			quota, clamp, err = info.OpenRouterCost.Basis.Quota(3)
			require.NoError(t, err)
			require.Nil(t, clamp)
			assert.Equal(t, 2250000, quota, "full excess is charged, not capped to the approximate hold")
			assert.Equal(t, 2.0, *info.OpenRouterCost.ApproximateReserveUSD)
			assert.Contains(t, recorder.Body.String(), "ok")
		})
	}
}

func TestOpenRouterModelReserveSettings(t *testing.T) {
	for _, tc := range []struct {
		model  string
		source dto.OpenRouterReservationSource
		valid  bool
	}{
		{"other/text", dto.OpenRouterReservationConfiguredPrice, true},
		{"other/text", dto.OpenRouterReservationUpstream, true},
		{"other/*", dto.OpenRouterReservationConfiguredPrice, false},
		{" other/text", dto.OpenRouterReservationConfiguredPrice, false},
		{"other/text", "unknown", false},
		{"other/text", "", false},
	} {
		settings := dto.ChannelSettings{OpenRouterReservationSource: map[string]dto.OpenRouterReservationSource{tc.model: tc.source}}
		if !tc.valid {
			require.Error(t, settings.ValidateOpenRouterReserves())
			continue
		}
		require.NoError(t, settings.ValidateOpenRouterReserves())
		encoded, err := common.Marshal(settings)
		require.NoError(t, err)
		var restored dto.ChannelSettings
		require.NoError(t, common.Unmarshal(encoded, &restored))
		assert.Equal(t, tc.source, restored.OpenRouterReservationSource[tc.model])
	}
	for _, tc := range []struct {
		name, model string
		reserve     float64
		invalid     bool
	}{
		{name: "second exact model", model: "other/reasoner", reserve: 2},
		{name: "zero", model: "other/reasoner", invalid: true},
		{name: "negative", model: "other/reasoner", reserve: -1, invalid: true},
		{name: "infinite", model: "other/reasoner", reserve: math.Inf(1), invalid: true},
		{name: "wildcard", model: "other/*", reserve: 2, invalid: true},
		{name: "whitespace", model: " other/reasoner", reserve: 2, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := dto.ChannelSettings{OpenRouterModelReserveUSD: map[string]float64{tc.model: tc.reserve}}
			if tc.invalid {
				require.Error(t, settings.ValidateOpenRouterReserves())
				return
			}
			require.NoError(t, settings.ValidateOpenRouterReserves())
			reserve := settings.OpenRouterReserveUSD(tc.model)
			require.NotNil(t, reserve)
			settings.OpenRouterModelReserveUSD[tc.model] = 7
			assert.Equal(t, 2.0, *reserve)
			assert.Nil(t, settings.OpenRouterReserveUSD(tc.model+"-alias"))
			otherChannel := dto.ChannelSettings{}
			assert.Nil(t, otherChannel.OpenRouterReserveUSD(tc.model))
		})
	}
}

func TestOpenRouterConfiguredPriceReservation(t *testing.T) {
	const modelID, alias = "other/local-reserve-text", "local-reserve-alias"
	for _, tc := range []struct {
		name, expression, extra, rateKey, rate, wantError string
		price, ratio, minimum                             float64
		source                                            dto.OpenRouterReservationSource
		noPolicy, wrongPolicy, noPrice, useRatio          bool
		missingCost, insufficient, knownAboveLocal        bool
		zeroTariffs                                       bool
	}{
		{name: "fixed configured price", price: 2},
		{name: "configured price preserves minimum", price: 2, minimum: 3},
		{name: "known upstream cost wins", price: 2, rateKey: "completion", rate: "1", knownAboveLocal: true},
		{name: "expression uses effective payload", expression: `param("max_tokens") == 128 ? tier("effective", fixed(2)) : tier("other", fixed(7))`},
		{name: "token expression uses USD per million", expression: `tier("tokens", c * 15625)`},
		{name: "expression receives request headers", expression: `header("x-reserve") == "yes" ? tier("header", fixed(2)) : tier("other", fixed(7))`},
		{name: "legacy ratio convention", useRatio: true, ratio: 5000},
		{name: "unknown reasoning accepted by positive estimate", price: 2, rateKey: "internal_reasoning", rate: "0.000003"},
		{name: "zero local does not erase upstream", price: 0},
		{name: "confirmed zero preserved", price: 0, zeroTariffs: true},
		{name: "zero cannot cover unknown quantity", rateKey: "internal_reasoning", rate: "0.000003", wantError: "requires a positive reserve"},
		{name: "disabled by default", price: 2, noPolicy: true},
		{name: "explicit upstream", price: 2, source: dto.OpenRouterReservationUpstream},
		{name: "policy keyed by final ID not alias", price: 2, wrongPolicy: true},
		{name: "invalid source rejected", price: 2, source: "invalid", wantError: "invalid OpenRouter reservation source"},
		{name: "missing configured price", noPrice: true, wantError: "has no configured reservation price"},
		{name: "negative configured price", price: -1, wantError: "invalid configured reservation price"},
		{name: "huge configured price", price: 1e100, wantError: "quota"},
		{name: "cannot recursively use provider cost", expression: `tier("provider", provider_cost * 1000000)`, wantError: "requires a validated provider cost estimate"},
		{name: "unmeasured audio expression rejected", expression: `p * 2 + ao * 50`, wantError: "unavailable ao quantity"},
		{name: "unmeasured task expression rejected", expression: `u("seconds") * 2`, wantError: "unavailable u quantity"},
		{name: "unknown dimension remains closed", price: 2, rateKey: "research_step", rate: "1", wantError: "unsupported billable dimension research_step"},
		{name: "missing tariff remains closed", price: 2, rateKey: "prompt", rate: "null", wantError: "invalid OpenRouter endpoint price"},
		{name: "stream stays closed", price: 2, extra: `,"stream":true`, wantError: "one nonstream text chat response"},
		{name: "image output stays closed", price: 2, extra: `,"modalities":["text","image"]`, wantError: "output modality tariff is unavailable"},
		{name: "tools stay closed", price: 2, extra: `,"tools":[{"type":"function"}]`, wantError: "one nonstream text chat response"},
		{name: "multiple responses stay closed", price: 2, extra: `,"n":2`, wantError: "one nonstream text chat response"},
		{name: "fallback model cannot disable selected policy", price: 2, extra: `,"models":["other/fallback"]`, wantError: "only supports one nonstream text chat response"},
		{name: "insufficient funds before paid POST", price: 2, insufficient: true, wantError: "insufficient test quota"},
		{name: "missing actual cost never becomes estimate", price: 2, missingCost: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldPrices, oldRatios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
			settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
			oldModes, oldExprs := settings.BillingMode, settings.BillingExpr
			t.Cleanup(func() {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
				settings.BillingMode, settings.BillingExpr = oldModes, oldExprs
			})
			settings.BillingMode, settings.BillingExpr = map[string]string{}, map[string]string{}
			if tc.expression != "" {
				settings.BillingMode[alias], settings.BillingExpr[alias] = billing_setting.BillingModeTieredExpr, tc.expression
			} else if !tc.noPrice {
				encoded, err := common.Marshal(map[string]float64{alias: tc.price})
				require.NoError(t, err)
				if tc.useRatio {
					encoded, err = common.Marshal(map[string]float64{alias: tc.ratio})
					require.NoError(t, err)
					require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(encoded)))
				} else {
					require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(encoded)))
				}
			}
			prices := map[string]any{"prompt": "0.000002", "completion": "0.000008"}
			if tc.zeroTariffs {
				prices["prompt"], prices["completion"] = "0", "0"
			}
			if tc.rateKey != "" {
				prices[tc.rateKey] = tc.rate
				if tc.rate == "null" {
					prices[tc.rateKey] = nil
				}
			}
			metadata, err := common.Marshal(map[string]any{"data": map[string]any{"id": modelID, "endpoints": []any{map[string]any{"tag": "other-provider", "model_id": modelID, "max_completion_tokens": 128, "pricing": prices}}}})
			require.NoError(t, err)
			transport := &sonarAdmissionTransport{metadata: string(metadata)}
			if tc.missingCost {
				transport.response = `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{}}`
			}
			client, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
			require.NoError(t, err)
			oldTransport := client.Transport
			client.Transport = transport
			t.Cleanup(func() { client.Transport = oldTransport })
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Reply OK."}],"max_tokens":128%s}`, modelID, tc.extra)
			c, recorder, _, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
			info.ChannelType, info.ChannelBaseUrl = constant.ChannelTypeOpenRouter, "https://openrouter.test/api"
			info.RequestURLPath, info.RelayMode = "/v1/chat/completions", relayconstant.RelayModeChatCompletions
			info.OriginModelName, info.UpstreamModelName = alias, modelID
			info.RequestHeaders = map[string]string{"X-Reserve": "yes"}
			info.PriceData.BillingGroupRatio = 1.5
			if !tc.noPolicy {
				policyID, source := modelID, tc.source
				if tc.wrongPolicy {
					policyID = alias
				}
				if source == "" {
					source = dto.OpenRouterReservationConfiguredPrice
				}
				info.ChannelSetting.OpenRouterReservationSource = map[string]dto.OpenRouterReservationSource{policyID: source}
			}
			if tc.minimum > 0 {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{modelID: tc.minimum}
			}
			billing := &sonarAdmissionBilling{}
			if tc.insufficient {
				billing.err = errors.New("insufficient test quota")
			}
			info.Billing = billing
			adaptor := &Adaptor{}
			response, err := adaptor.DoRequest(c, info, strings.NewReader(body))
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Zero(t, transport.posts)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, transport.posts)
			assert.JSONEq(t, body, string(transport.body))
			require.NotNil(t, info.OpenRouterCost)
			assert.Nil(t, info.TieredBillingSnapshot)
			assert.Nil(t, info.BillingRequestInput)
			if tc.noPolicy || tc.wrongPolicy || tc.source == dto.OpenRouterReservationUpstream {
				assert.Nil(t, info.OpenRouterCost.ConfiguredReserveUSD)
				assert.Less(t, info.OpenRouterCost.Basis.EstimatedUSD, 2.0)
			} else {
				require.NotNil(t, info.OpenRouterCost.ConfiguredReserveUSD)
				assert.Equal(t, alias, info.OpenRouterCost.ConfiguredReserveModel)
				assert.GreaterOrEqual(t, info.OpenRouterCost.Basis.EstimatedUSD, *info.OpenRouterCost.ConfiguredReserveUSD)
				assert.GreaterOrEqual(t, info.OpenRouterCost.Basis.EstimatedUSD, tc.minimum)
				if tc.expression != "" {
					assert.Equal(t, 2.0, *info.OpenRouterCost.ConfiguredReserveUSD)
					assert.NotEmpty(t, info.OpenRouterCost.ConfiguredReserveExprHash)
				} else if !tc.useRatio {
					assert.Equal(t, tc.price, *info.OpenRouterCost.ConfiguredReserveUSD)
				} else {
					assert.Greater(t, *info.OpenRouterCost.ConfiguredReserveUSD, 1.0)
				}
				if !tc.knownAboveLocal && !tc.useRatio && !tc.zeroTariffs && tc.price > 0 {
					assert.Equal(t, max(tc.price, tc.minimum), info.OpenRouterCost.Basis.EstimatedUSD)
				}
			}
			if tc.knownAboveLocal {
				assert.Greater(t, info.OpenRouterCost.Basis.EstimatedUSD, 128.0)
			}
			info.PriceData.BillingGroupRatio = 9
			usage, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
			if tc.missingCost {
				require.NotNil(t, apiErr)
				assert.False(t, info.OpenRouterCost.Final)
				assert.Empty(t, recorder.Body.String())
				return
			}
			require.Nil(t, apiErr)
			assert.Equal(t, .47545, usage.(*dto.Usage).Cost)
			quota, clamp, err := info.OpenRouterCost.Basis.Quota(3)
			require.NoError(t, err)
			require.Nil(t, clamp)
			assert.Equal(t, 2250000, quota, "actual may exceed the hold, using the frozen ratio once")
		})
	}
}
