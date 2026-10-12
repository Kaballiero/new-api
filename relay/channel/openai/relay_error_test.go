package openai

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
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
