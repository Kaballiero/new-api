package common_test

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	cl "github.com/QuantumNous/new-api/relay/channel/claude"
	ge "github.com/QuantumNous/new-api/relay/channel/gemini"
	oa "github.com/QuantumNous/new-api/relay/channel/openai"
	rc "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestProjectClientResponseSanitizesProtocolMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.BindClientResponseModel(c, "client/model:variant")

	body := []byte(`{"model":"upstream","provider":"kept","fallback":"kept","usage":{"prompt_tokens":1,"cost":2,"cost_details":{"x":1},"is_byok":true,"usage_semantic":"openai","usage_source":"provider","billing_usage":{"source":"provider"}},"response":{"model":"upstream-response","usage":{"completion_tokens":2,"cost":3}},"message":{"model":"upstream-message","usage":{"input_tokens":3,"billing_usage":{"semantic":"anthropic"}}},"session":{"model":"upstream-session","usage":{"output_tokens":4,"usage_source":"provider"}},"usageMetadata":{"totalTokenCount":5,"cost":6},"content":{"cost":7,"model":"content-model"}}`)

	projected, err := common.ProjectClientResponse(c, body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"client/model:variant","provider":"kept","fallback":"kept","usage":{"prompt_tokens":1},"response":{"model":"client/model:variant","usage":{"completion_tokens":2}},"message":{"model":"client/model:variant","usage":{"input_tokens":3}},"session":{"model":"client/model:variant","usage":{"output_tokens":4}},"usageMetadata":{"totalTokenCount":5},"content":{"cost":7,"model":"content-model"}}`, string(projected))
}

func TestProjectClientResponsePreservesUnboundModelAndUnknownContent(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	body := []byte(`{"model":"upstream","content":{"cost":7,"billing_usage":{"source":"content"}},"usage":{"cost":1,"prompt_tokens":2}}`)

	projected, err := common.ProjectClientResponse(c, body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"upstream","content":{"cost":7,"billing_usage":{"source":"content"}},"usage":{"prompt_tokens":2}}`, string(projected))
}

func TestProjectClientResponseOmitsKnownModelWhenBoundIdentityIsUnknown(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	common.BindClientResponseModel(c, "")

	projected, err := common.ProjectClientResponse(c, []byte(`{"model":"upstream","response":{"model":"upstream-response"},"content":{"model":"content-model"}}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"response":{},"content":{"model":"content-model"}}`, string(projected))
}

func TestProjectClientResponseRejectsDuplicateMetadataKeys(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	common.BindClientResponseModel(c, "client-model")

	_, err := common.ProjectClientResponse(c, []byte(`{"usage":{"cost":1,"cost":2},"model":"upstream","model":"upstream-duplicate"}`))
	require.Error(t, err)
}

func TestProjectClientResponseSanitizesEscapedMetadataKey(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)

	projected, err := common.ProjectClientResponse(c, []byte(`{"usage":{"\u0063ost":1,"prompt_tokens":2}}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"usage":{"prompt_tokens":2}}`, string(projected))
}

func TestWriteClientJSONProjectsKnownTaskEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	common.BindClientResponseModel(c, "public:modifier")
	err := common.WriteClientJSON(c, http.StatusOK, map[string]any{
		"data": map[string]any{
			"data": map[string]any{
				"model": "private-upstream",
				"usage": map[string]any{"prompt_tokens": 1, "cost": 2, "billing_usage": map[string]any{"source": "private"}},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"data":{"data":{"model":"public:modifier","usage":{"prompt_tokens":1}}}}`, recorder.Body.String())
}

const deny = `"cost":0.75,"cost_details":{"upstream_inference_cost":0.7},"is_byok":true,"usage_semantic":"openai","usage_source":"provider","billing_usage":{"source":"openai_chat","semantic":"openai","estimated":false,"openai_usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`
const alias = "public-alias:thinking-high"

func privacyFixture(src string, stream bool) string {
	u := `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":0,"audio_tokens":3},"unknown_tokens":42,` + deny + `}`
	cu := `{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":3,"cache_creation":{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":0},"unknown_tokens":42,` + deny + `}`
	gu := `{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12,"cachedContentTokenCount":0,"thoughtsTokenCount":1,"unknown_tokens":42,` + deny + `}`
	if src == "openai" {
		if !stream {
			return `{"id":"x","model":"mapped-upstream","provider":"keep-provider","fallback":"keep-fallback","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":` + u + `}`
		}
		return "data: " + `{"id":"x","model":"mapped-upstream","provider":"keep-provider","fallback":"keep-fallback","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}],"usage":` + u + `}` + "\n\ndata: " + `{"id":"x","model":"mapped-upstream","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":` + u + `}` + "\n\ndata: [DONE]\n\n"
	}
	if src == "claude" {
		if !stream {
			return `{"id":"x","type":"message","role":"assistant","model":"mapped-upstream","provider":"keep-provider","fallback":"keep-fallback","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":` + cu + `}`
		}
		return "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"x","type":"message","role":"assistant","model":"mapped-upstream","content":[],"usage":` + cu + `},"provider":"keep-provider","fallback":"keep-fallback"}` + "\n\nevent: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\nevent: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":` + cu + `}` + "\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	}
	b := `{"modelVersion":"mapped-upstream","provider":"keep-provider","fallback":"keep-fallback","candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":` + gu + `}`
	if stream {
		return "data: " + b + "\n\ndata: " + b + "\n\n"
	}
	return b
}

func privacyRelayContext(t *testing.T, target string, stream bool) (*gin.Context, *httptest.ResponseRecorder, *rc.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, alias)
	info := rc.GenRelayInfoOpenAI(c, nil)
	info.ChannelMeta = &rc.ChannelMeta{UpstreamModelName: "mapped-upstream"}
	info.BillingModelName = "billing-normalized"
	info.IsStream = stream
	info.ShouldIncludeUsage = true
	info.SetEstimatePromptTokens(10)
	info.RelayFormat = types.RelayFormatOpenAI
	if target == "claude" {
		info.RelayFormat = types.RelayFormatClaude
	}
	if target == "gemini" {
		info.RelayFormat = types.RelayFormatGemini
	}
	info.EnsureClaudeConvertInfo()
	return c, recorder, info
}

func TestClientPrivacyHandlerMatrix(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, source := range []string{"openai", "claude", "gemini"} {
		for _, target := range []string{"openai", "claude", "gemini"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-to-%s-stream-%t", source, target, stream), func(t *testing.T) {
					c, recorder, info := privacyRelayContext(t, target, stream)
					c.Header(common.RequestIdKey, "local-request")
					c.Header("Set-Cookie", "local=session; HttpOnly")
					c.Header("Access-Control-Expose-Headers", common.RequestIdKey)
					c.Header("Access-Control-Allow-Origin", "https://client.example")
					body := privacyFixture(source, stream)
					resp := &http.Response{StatusCode: 200, Header: http.Header{
						"Content-Type": {"application/json"}, "x-generation-id": {"private"}, "cF-rAy": {"private"},
						"X-Provider-Name": {"private"}, "Access-Control-Expose-Headers": {"private"},
						"Set-Cookie": {"foreign=yes; Domain=provider.example", "hostile=yes"}, common.RequestIdKey: {"upstream-request"}, "X-Safe": {"safe"},
					}, Body: io.NopCloser(strings.NewReader(body))}
					var usage *dto.Usage
					var apiErr *types.NewAPIError
					switch source {
					case "openai":
						if stream {
							usage, apiErr = oa.OaiStreamHandler(c, info, resp)
						} else {
							usage, apiErr = oa.OpenaiHandler(c, info, resp)
						}
					case "claude":
						if stream {
							usage, apiErr = cl.ClaudeStreamHandler(c, resp, info)
						} else {
							usage, apiErr = cl.ClaudeHandler(c, resp, info)
						}
					case "gemini":
						if target == "gemini" {
							if stream {
								usage, apiErr = ge.GeminiTextGenerationStreamHandler(c, info, resp)
							} else {
								usage, apiErr = ge.GeminiTextGenerationHandler(c, info, resp)
							}
						} else {
							if stream {
								usage, apiErr = ge.GeminiChatStreamHandler(c, info, resp)
							} else {
								usage, apiErr = ge.GeminiChatHandler(c, info, resp)
							}
						}
					}
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					assert.Equal(t, 10, usage.PromptTokens)
					expectedCompletion := 2
					if source == "gemini" {
						expectedCompletion = 3
					}
					assert.Equal(t, expectedCompletion, usage.CompletionTokens)
					require.NotNil(t, usage.BillingUsage, "internal billing sidecar survives")
					if source == "openai" {
						require.NotNil(t, usage.Cost)
						assert.Equal(t, 0.75, usage.Cost)
						assert.Equal(t, "provider", usage.UsageSource)
					}
					assert.Equal(t, alias, info.OriginModelName)
					assert.Equal(t, "billing-normalized", info.BillingModelName)
					assert.Equal(t, http.StatusOK, recorder.Code)
					assert.Equal(t, "local-request", recorder.Header().Get(common.RequestIdKey))
					assert.Equal(t, []string{"local=session; HttpOnly"}, recorder.Header().Values("Set-Cookie"))
					assert.Equal(t, common.RequestIdKey, recorder.Header().Get("Access-Control-Expose-Headers"))
					assert.Equal(t, "https://client.example", recorder.Header().Get("Access-Control-Allow-Origin"))
					for _, name := range []string{"X-Generation-Id", "CF-Ray", "X-Provider-Name"} {
						assert.Empty(t, recorder.Header().Get(name))
					}
					if !stream {
						assert.Equal(t, "upstream-request", c.GetString(common.UpstreamRequestIdKey))
						assert.Equal(t, fmt.Sprint(recorder.Body.Len()), recorder.Header().Get("Content-Length"))
					}
					events := []string{recorder.Body.String()}
					if stream {
						events = nil
						for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
							if event, ok := strings.CutPrefix(line, "data: "); ok && event != "[DONE]" {
								events = append(events, event)
							}
						}
					}
					require.NotEmpty(t, events)
					modelCount := 0
					for _, event := range events {
						require.True(t, gjson.Valid(event), event)
						for _, prefix := range []string{"", "response.", "message.", "session."} {
							for _, field := range []string{"model", "modelVersion"} {
								if value := gjson.Get(event, prefix+field); value.Exists() {
									assert.Equal(t, alias, value.String())
									modelCount++
								}
							}
							for _, usagePath := range []string{"", "usage.", "usageMetadata."} {
								for _, field := range []string{"cost", "cost_details", "is_byok", "usage_semantic", "usage_source", "billing_usage"} {
									assert.False(t, gjson.Get(event, prefix+usagePath+field).Exists(), event)
								}
							}
						}
					}
					if target != "gemini" || source == "gemini" {
						require.Positive(t, modelCount)
					}
					if source == target {
						assert.Contains(t, recorder.Body.String(), `"provider":"keep-provider"`)
						assert.Contains(t, recorder.Body.String(), `"fallback":"keep-fallback"`)
						assert.Contains(t, recorder.Body.String(), `"unknown_tokens":42`)
						if source == "openai" {
							assert.Contains(t, recorder.Body.String(), `"cached_tokens":0`)
						}
						if source == "claude" {
							assert.Contains(t, recorder.Body.String(), `"ephemeral_1h_input_tokens":0`)
						}
						if source == "gemini" {
							assert.Contains(t, recorder.Body.String(), `"cachedContentTokenCount":0`)
						}
					}
				})
			}
		}
	}
}

func TestClientPrivacyJSONClassificationAndOpaqueFormats(t *testing.T) {
	body := " \t\r\n" + privacyFixture("openai", false)
	for _, contentType := range []string{"", "text/plain", "application/problem+json", "application/json"} {
		t.Run(contentType, func(t *testing.T) {
			c, recorder, info := privacyRelayContext(t, "openai", false)
			usage, apiErr := oa.OpenaiHandler(c, info, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, apiErr)
			require.NotNil(t, usage.Cost)
			assert.Equal(t, alias, gjson.Get(recorder.Body.String(), "model").String())
			assert.False(t, gjson.Get(recorder.Body.String(), "usage.cost").Exists())
		})
	}
	for _, format := range []string{"text", "srt", "vtt"} {
		for _, body := range []string{`{"model":"spoken","cost":12}`, "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nhello", "RIFF\x00\xffsynthetic", ""} {
			c, recorder, info := privacyRelayContext(t, "openai", false)
			apiErr, _ := oa.OpenaiSTTHandler(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, info, format)
			require.Nil(t, apiErr)
			assert.Equal(t, body, recorder.Body.String())
		}
	}
}

func TestClientPrivacyRejectsAmbiguousMetadataBeforeFraming(t *testing.T) {
	for _, body := range []string{"", " ", `{BROKEN`, `{"response":{"usage":{"cost":1},"us\u0061ge":{"cost":2}}}`, `{"data":{"data":{"usage":{"cost":1,"\u0063ost":2}}}}`, `{"result":{"usageMetadata":{"cost":1,"cost":2}}}`} {
		c, recorder, _ := privacyRelayContext(t, "openai", false)
		err := service.IOCopyBytesGracefully(c, nil, []byte(body))
		require.Error(t, err)
		assert.Equal(t, 502, recorder.Code)
		assert.Empty(t, recorder.Body.String())
		c, recorder, _ = privacyRelayContext(t, "openai", true)
		err = helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.created"}, body)
		require.Error(t, err)
		assert.Empty(t, recorder.Body.String())
	}
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, source := range []string{"openai", "claude", "gemini"} {
		c, recorder, info := privacyRelayContext(t, source, true)
		body := `{"model":"private","usage":{"prompt_tokens":1,"cost":1,"cost":2}}`
		if source == "claude" {
			body = `{"type":"message_start","message":{"model":"private","usage":{"input_tokens":1,"cost":1,"cost":2}}}`
		}
		if source == "gemini" {
			body = `{"modelVersion":"private","usageMetadata":{"promptTokenCount":1,"cost":1,"cost":2}}`
		}
		resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: " + body + "\n\n"))}
		var apiErr *types.NewAPIError
		switch source {
		case "openai":
			_, apiErr = oa.OaiStreamHandler(c, info, resp)
		case "claude":
			_, apiErr = cl.ClaudeStreamHandler(c, resp, info)
		case "gemini":
			_, apiErr = ge.GeminiTextGenerationStreamHandler(c, info, resp)
		}
		require.NotNil(t, apiErr, source)
		assert.Empty(t, recorder.Body.String(), source)
		assert.True(t, info.StreamStatus.HasErrors(), source)
	}
}

func TestClientPrivacyKnownEnvelopesAndOpaqueArtifacts(t *testing.T) {
	c, _, _ := privacyRelayContext(t, "openai", false)
	for _, envelope := range []string{"", "response", "message", "session", "data", "data.data", "result"} {
		for _, metadata := range []string{"", "usage", "usageMetadata"} {
			body := `{"model":"private","artifact":{"model":"user","cost":1,"cost":2,"usage":{"cost":3},"n":1e400},` + deny + `}`
			if metadata != "" {
				body = `{"` + metadata + `":` + body + `}`
			}
			parts := strings.Split(envelope, ".")
			if envelope != "" {
				for i := len(parts) - 1; i >= 0; i-- {
					body = `{"` + parts[i] + `":` + body + `}`
				}
			}
			original := []byte(body)
			projected, err := common.ProjectClientResponse(c, original)
			require.NoError(t, err)
			assert.Equal(t, body, string(original))
			prefix := strings.Trim(strings.Join([]string{envelope, metadata}, "."), ".")
			if prefix != "" {
				prefix += "."
			}
			for _, field := range []string{"cost", "cost_details", "is_byok", "usage_semantic", "usage_source", "billing_usage"} {
				assert.False(t, gjson.GetBytes(projected, prefix+field).Exists(), string(projected))
			}
			assert.Contains(t, string(projected), `{"model":"user","cost":1,"cost":2,"usage":{"cost":3},"n":1e400}`)
		}
	}
	body := []byte(`[{"model":"private","usage":{"cost":1}},{"model":"private","usageMetadata":{"billing_usage":{}}}]`)
	result, err := common.ProjectClientResponse(c, body)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"model":"`+alias+`","usage":{}},{"model":"`+alias+`","usageMetadata":{}}]`, string(result))
}

func TestClientPrivacyResponsesAndImageHandlers(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	response := `{"id":"resp_x","model":"mapped-private","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,` + deny + `}}`
	for _, kind := range []string{"responses", "compact", "responses-stream", "image", "embedding"} {
		t.Run(kind, func(t *testing.T) {
			c, recorder, info := privacyRelayContext(t, "openai", kind == "responses-stream")
			body := response
			if kind == "responses-stream" {
				body = "data: " + `{"type":"response.created","response":` + response + `}` + "\n\ndata: " + `{"type":"response.completed","response":` + response + `}` + "\n\n"
			}
			if kind == "image" {
				body = `{"model":"mapped-private","data":[{"b64_json":"aW1hZ2U=","revised_prompt":"cost model"}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,` + deny + `}}`
			}
			if kind == "embedding" {
				body = `{"modelVersion":"mapped-private","embedding":{"values":[0,1]},"usageMetadata":{"totalTokenCount":10,` + deny + `}}`
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader(body))}
			var usage *dto.Usage
			var apiErr *types.NewAPIError
			switch kind {
			case "responses":
				usage, apiErr = oa.OaiResponsesHandler(c, info, resp)
			case "compact":
				usage, apiErr = oa.OaiResponsesCompactionHandler(c, resp)
			case "responses-stream":
				usage, apiErr = oa.OaiResponsesStreamHandler(c, info, resp)
			case "image":
				usage, apiErr = oa.OpenaiImageHandler(c, info, resp)
			case "embedding":
				usage, apiErr = ge.NativeGeminiEmbeddingHandler(c, resp, info)
			}
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.NotContains(t, recorder.Body.String(), "mapped-private")
			assert.Contains(t, recorder.Body.String(), alias)
			for _, field := range []string{"cost", "cost_details", "is_byok", "usage_semantic", "usage_source", "billing_usage"} {
				assert.NotContains(t, recorder.Body.String(), `"`+field+`":`)
			}
			if kind == "responses" || kind == "responses-stream" {
				require.NotNil(t, usage.BillingUsage)
			}
			if kind == "image" {
				assert.Equal(t, "aW1hZ2U=", gjson.Get(recorder.Body.String(), "data.0.b64_json").String())
			}
		})
	}
	c, recorder, info := privacyRelayContext(t, "openai", true)
	bad := `{"type":"response.created","response":{"usage":{"cost":1},"usage":{"cost":2}}}`
	_, apiErr := oa.OaiResponsesStreamHandler(c, info, &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: " + bad + "\n\n"))})
	require.NotNil(t, apiErr)
	assert.Empty(t, recorder.Body.String())
	assert.True(t, info.StreamStatus.HasErrors())
}

func TestClientPrivacyOpaqueAudioAndRealtimeDirection(t *testing.T) {
	c, recorder, info := privacyRelayContext(t, "openai", false)
	info.Request = &dto.AudioRequest{ResponseFormat: "pcm"}
	body := "\x00\xff{\"model\":\"audio\",\"cost\":1}"
	c.Header("Set-Cookie", "local=session")
	usage := oa.OpenaiTTSHandler(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"audio/pcm"}, "CF-Ray": {"private"}, "Set-Cookie": {"foreign=yes; Domain=provider.invalid", "hostile=yes"}}, Body: io.NopCloser(strings.NewReader(body))}, info)
	require.NotNil(t, usage)
	assert.Equal(t, body, recorder.Body.String())
	assert.Equal(t, "audio/pcm", recorder.Header().Get("Content-Type"))
	assert.Empty(t, recorder.Header().Get("CF-Ray"))
	assert.Equal(t, []string{"local=session"}, recorder.Header().Values("Set-Cookie"))
	router := gin.New()
	writeErrors := make(chan error, 2)
	router.GET("/", func(c *gin.Context) {
		common.BindClientResponseModel(c, alias)
		upgrader := websocket.Upgrader{}
		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			writeErrors <- err
			return
		}
		defer ws.Close()
		writeErrors <- helper.WssClientString(c, ws, `{"type":"session.created","session":{"model":"private","usage":{"total_tokens":3,`+deny+`}}}`)
		writeErrors <- helper.WssString(c, ws, `{"model":"upstream-request","cost":7}`)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer ws.Close()
	_, downstream, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"session.created","session":{"model":"`+alias+`","usage":{"total_tokens":3}}}`, string(downstream))
	_, upstream, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, `{"model":"upstream-request","cost":7}`, string(upstream))
	require.NoError(t, <-writeErrors)
	require.NoError(t, <-writeErrors)
}
