package openai

import (
	"errors"
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

			require.Nil(t, relayError)
			require.NotNil(t, usage)
			require.Contains(t, recorder.Body.String(), tt.data)
			if tt.name == "malformed" {
				require.True(t, info.StreamStatus.HasErrors())
			} else {
				require.False(t, info.StreamStatus.HasErrors())
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
