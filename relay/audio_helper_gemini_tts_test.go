package relay

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const geminiTTSHelperModel = "gemini-2.5-flash-preview-tts"

type geminiTTSHelperRun struct {
	relayErr      *types.NewAPIError
	upstreamCalls int
	recorder      *httptest.ResponseRecorder
}

func runGeminiTTSAudioHelper(t *testing.T, request dto.AudioRequest, upstream http.HandlerFunc) geminiTTSHelperRun {
	return runGeminiTTSAudioHelperWithResponseLimit(t, request, 1, upstream)
}

func runGeminiTTSAudioHelperWithResponseLimit(t *testing.T, request dto.AudioRequest, maxFileDownloadMB int, upstream http.HandlerFunc) geminiTTSHelperRun {
	t.Helper()
	service.InitHttpClient()
	previousMaxFileDownloadMB := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = maxFileDownloadMB
	t.Cleanup(func() {
		constant.MaxFileDownloadMB = previousMaxFileDownloadMB
	})

	run := geminiTTSHelperRun{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		run.upstreamCalls++
		upstream(w, r)
	}))
	t.Cleanup(server.Close)

	gin.SetMode(gin.TestMode)
	run.recorder = httptest.NewRecorder()
	c, _ := gin.CreateTestContext(run.recorder)

	body, err := common.Marshal(request)
	require.NoError(t, err)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	common.SetContextKey(c, constant.ContextKeyOriginalModel, request.Model)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeGemini)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-gemini-key")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAudioSpeech,
		RelayFormat:     types.RelayFormatOpenAIAudio,
		OriginModelName: request.Model,
		Request:         &request,
	}

	run.relayErr = AudioHelper(c, info)
	return run
}

func TestAudioHelperGeminiTTSReachesUpstreamForAcceptedRequest(t *testing.T) {
	run := runGeminiTTSAudioHelper(t, dto.AudioRequest{
		Model: geminiTTSHelperModel, Input: "hello world", Voice: "alloy", ResponseFormat: "wav",
	}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream unavailable"}}`))
	})

	require.NotNil(t, run.relayErr)
	assert.Equal(t, 1, run.upstreamCalls)
	assert.NotEqual(t, types.ErrorCodeInvalidRequest, run.relayErr.GetErrorCode())
}

func TestAudioHelperGeminiTTSAcceptsSpeedOneAndAbsentInstructions(t *testing.T) {
	speedOne := 1.0

	run := runGeminiTTSAudioHelper(t, dto.AudioRequest{
		Model: geminiTTSHelperModel, Input: "hello world", Voice: "shimmer", ResponseFormat: "pcm", Speed: &speedOne,
	}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	require.NotNil(t, run.relayErr)
	assert.Equal(t, 1, run.upstreamCalls)
	assert.NotEqual(t, types.ErrorCodeInvalidRequest, run.relayErr.GetErrorCode())
}

func TestAudioHelperGeminiTTSRejectsInvalidResponseLimitBeforeUpstream(t *testing.T) {
	run := runGeminiTTSAudioHelperWithResponseLimit(t, dto.AudioRequest{
		Model: geminiTTSHelperModel, Input: "hello world", Voice: "alloy", ResponseFormat: "wav",
	}, 0, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	require.NotNil(t, run.relayErr)
	assert.Equal(t, 0, run.upstreamCalls)
	assert.True(t, types.IsSkipRetryError(run.relayErr))
	assert.Contains(t, run.relayErr.Error(), "invalid MAX_FILE_DOWNLOAD_MB configuration")
	assert.Empty(t, run.recorder.Body.Bytes())
}

func TestAudioHelperGeminiTTSRejectsInvalidRequestBeforeUpstream(t *testing.T) {
	speedTwo := 2.0
	speedZero := 0.0

	cases := []struct {
		name    string
		request dto.AudioRequest
	}{
		{name: "empty input", request: dto.AudioRequest{Model: geminiTTSHelperModel, Voice: "alloy", ResponseFormat: "wav"}},
		{name: "whitespace only input", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: " \n\t ", Voice: "alloy", ResponseFormat: "wav"}},
		{name: "unknown voice", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "verse", ResponseFormat: "wav"}},
		{name: "native gemini voice", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "Kore", ResponseFormat: "wav"}},
		{name: "omitted response format", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "alloy"}},
		{name: "mp3 response format", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "alloy", ResponseFormat: "mp3"}},
		{name: "uppercase response format", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "alloy", ResponseFormat: "WAV"}},
		{name: "speed above one", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "alloy", ResponseFormat: "wav", Speed: &speedTwo}},
		{name: "speed zero", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "alloy", ResponseFormat: "wav", Speed: &speedZero}},
		{name: "sse stream format", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "alloy", ResponseFormat: "wav", StreamFormat: "sse"}},
		{name: "instructions", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "alloy", ResponseFormat: "wav", Instructions: "speak slowly"}},
		{name: "whitespace only instructions", request: dto.AudioRequest{Model: geminiTTSHelperModel, Input: "hi", Voice: "alloy", ResponseFormat: "wav", Instructions: "  "}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runGeminiTTSAudioHelper(t, tc.request, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			require.NotNil(t, run.relayErr)
			assert.Equal(t, 0, run.upstreamCalls)
			assert.Equal(t, http.StatusBadRequest, run.relayErr.StatusCode)
			assert.Equal(t, types.ErrorCodeInvalidRequest, run.relayErr.GetErrorCode())
			assert.True(t, types.IsSkipRetryError(run.relayErr))
			assert.Empty(t, run.recorder.Body.Bytes())
		})
	}
}
