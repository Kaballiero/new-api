package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	geminiTTSFlashModel = "gemini-2.5-flash-preview-tts"
	geminiTTSProModel   = "gemini-2.5-pro-preview-tts"
	geminiTTSMimeL16    = "audio/L16;codec=pcm;rate=24000"
)

var geminiTTSSamplePCM = []byte{0x01, 0x00, 0xff, 0x7f, 0x00, 0x80}

func geminiTTSAudioRequest(voice, responseFormat string) dto.AudioRequest {
	return dto.AudioRequest{
		Model:          geminiTTSFlashModel,
		Input:          "hello world",
		Voice:          voice,
		ResponseFormat: responseFormat,
	}
}

func geminiTTSTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	previousMaxFileDownloadMB := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() {
		constant.MaxFileDownloadMB = previousMaxFileDownloadMB
	})
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewReader([]byte(`{}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func geminiTTSRelayInfo(model, responseFormat string) *relaycommon.RelayInfo {
	request := geminiTTSAudioRequest("alloy", responseFormat)
	request.Model = model
	return &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAudioSpeech,
		RelayFormat:     types.RelayFormatOpenAIAudio,
		OriginModelName: model,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: model},
		Request:         &request,
	}
}

func geminiTTSResponse(t *testing.T, mimeType string, pcm []byte, metadata dto.GeminiUsageMetadata) *http.Response {
	t.Helper()
	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{
							InlineData: &dto.GeminiInlineData{
								MimeType: mimeType,
								Data:     base64.StdEncoding.EncodeToString(pcm),
							},
						},
					},
				},
			},
		},
		UsageMetadata: metadata,
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
}

func geminiTTSRawResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(body)))}
}

func TestGeminiTTSHandlerRejectsOversizedResponse(t *testing.T) {
	const maxResponseBytes = int64(1 << 20)
	validResponse := geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, geminiTTSAudioUsageMetadata())
	validBody, err := io.ReadAll(validResponse.Body)
	require.NoError(t, err)
	require.Less(t, int64(len(validBody)), maxResponseBytes+1)
	oversizedBody := append(validBody, bytes.Repeat([]byte{' '}, int(maxResponseBytes+1)-len(validBody))...)

	cases := []struct {
		name          string
		response      *http.Response
		errorContains string
	}{
		{
			name:          "declared size",
			errorContains: "response size",
			response: &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: maxResponseBytes + 1,
				Body:          io.NopCloser(strings.NewReader(`{}`)),
			},
		},
		{
			name:          "actual size",
			errorContains: "response exceeds",
			response: &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: -1,
				Body: io.NopCloser(io.MultiReader(
					bytes.NewReader(oversizedBody),
					iotest.ErrReader(errors.New("read beyond response limit")),
				)),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder := geminiTTSTestContext(t)
			info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")

			usage, apiErr := GeminiTTSHandler(c, info, tc.response)

			require.NotNil(t, apiErr)
			assert.Nil(t, usage)
			assert.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Contains(t, apiErr.Error(), tc.errorContains)
			assert.Empty(t, recorder.Body.Bytes())
		})
	}
}

func TestGeminiTTSHandlerDoesNotRetryInvalidResponseLimitConfiguration(t *testing.T) {
	c, recorder := geminiTTSTestContext(t)
	constant.MaxFileDownloadMB = 0
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")
	response := geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, geminiTTSAudioUsageMetadata())

	usage, apiErr := GeminiTTSHandler(c, info, response)

	require.NotNil(t, apiErr)
	assert.Nil(t, usage)
	assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.Empty(t, recorder.Body.Bytes())
}

func TestGeminiTTSHandlerDoesNotRetryLocalInvariantErrors(t *testing.T) {
	unsupportedFormatRequest := geminiTTSAudioRequest("alloy", "mp3")
	cases := []struct {
		name    string
		request dto.Request
	}{
		{name: "wrong request type", request: &dto.BaseRequest{}},
		{name: "unsupported response format", request: &unsupportedFormatRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder := geminiTTSTestContext(t)
			info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")
			info.Request = tc.request
			response := geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, geminiTTSAudioUsageMetadata())

			usage, apiErr := GeminiTTSHandler(c, info, response)

			require.NotNil(t, apiErr)
			assert.Nil(t, usage)
			assert.True(t, types.IsSkipRetryError(apiErr))
			assert.Empty(t, recorder.Body.Bytes())
		})
	}
}

func TestGeminiTTSHandlerAcceptsResponseAtSizeLimit(t *testing.T) {
	const maxResponseBytes = 1 << 20

	c, recorder := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")
	response := geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, geminiTTSAudioUsageMetadata())
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Less(t, len(body), maxResponseBytes)
	body = append(body, bytes.Repeat([]byte{' '}, maxResponseBytes-len(body))...)
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))

	usage, apiErr := GeminiTTSHandler(c, info, response)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, geminiTTSSamplePCM, recorder.Body.Bytes())
}

func geminiTTSAudioUsageMetadata() dto.GeminiUsageMetadata {
	return dto.GeminiUsageMetadata{
		PromptTokenCount:     100,
		CandidatesTokenCount: 200,
		TotalTokenCount:      300,
		PromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "TEXT", TokenCount: 100},
		},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "AUDIO", TokenCount: 200},
		},
	}
}

func TestGeminiConvertAudioRequestBuildsGenerateContentPayload(t *testing.T) {
	voices := []struct {
		openAIVoice string
		geminiVoice string
	}{
		{"alloy", "Kore"},
		{"echo", "Charon"},
		{"fable", "Puck"},
		{"onyx", "Gacrux"},
		{"nova", "Aoede"},
		{"shimmer", "Leda"},
	}
	models := []string{geminiTTSFlashModel, geminiTTSProModel}

	adaptor := &Adaptor{}
	for _, model := range models {
		for _, voice := range voices {
			t.Run(model+"/"+voice.openAIVoice, func(t *testing.T) {
				c, _ := geminiTTSTestContext(t)
				info := geminiTTSRelayInfo(model, "wav")
				request := geminiTTSAudioRequest(voice.openAIVoice, "wav")
				request.Model = model

				reader, err := adaptor.ConvertAudioRequest(c, info, request)
				require.NoError(t, err)
				require.NotNil(t, reader)

				body, err := io.ReadAll(reader)
				require.NoError(t, err)

				assert.Equal(t, "hello world", gjson.GetBytes(body, "contents.0.parts.0.text").String())
				assert.Equal(t, "user", gjson.GetBytes(body, "contents.0.role").String())
				assert.Equal(t, []string{"AUDIO"}, gjsonStrings(gjson.GetBytes(body, "generationConfig.responseModalities")))
				assert.Equal(t, voice.geminiVoice,
					gjson.GetBytes(body, "generationConfig.speechConfig.voiceConfig.prebuiltVoiceConfig.voiceName").String())
				assert.False(t, gjson.GetBytes(body, "generationConfig.speechConfig.multiSpeakerVoiceConfig").Exists())
			})
		}
	}
}

func TestGeminiConvertAudioRequestAppliesSafetySettings(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	previousSafetySettings := settings.SafetySettings
	settings.SafetySettings = map[string]string{
		"default":                         "BLOCK_NONE",
		"HARM_CATEGORY_HARASSMENT":        "BLOCK_ONLY_HIGH",
		"HARM_CATEGORY_SEXUALLY_EXPLICIT": "BLOCK_MEDIUM_AND_ABOVE",
		"HARM_CATEGORY_DANGEROUS_CONTENT": "BLOCK_LOW_AND_ABOVE",
	}
	t.Cleanup(func() {
		settings.SafetySettings = previousSafetySettings
	})

	c, _ := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, info, geminiTTSAudioRequest("alloy", "pcm"))
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	for index, category := range SafetySettingList {
		path := fmt.Sprintf("safetySettings.%d", index)
		assert.Equal(t, category, gjson.GetBytes(body, path+".category").String())
		assert.Equal(t, model_setting.GetGeminiSafetySetting(category), gjson.GetBytes(body, path+".threshold").String())
	}
}

func TestGeminiConvertAudioRequestRejectsUnsupportedModel(t *testing.T) {
	c, _ := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo("gemini-2.5-flash", "wav")
	request := geminiTTSAudioRequest("alloy", "wav")
	request.Model = "gemini-2.5-flash"

	reader, err := (&Adaptor{}).ConvertAudioRequest(c, info, request)

	require.Error(t, err)
	assert.Nil(t, reader)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
}

func TestGeminiConvertAudioRequestPayloadIsIndependentOfResponseFormat(t *testing.T) {
	adaptor := &Adaptor{}
	bodies := make(map[string]string, 2)

	for _, format := range []string{"pcm", "wav"} {
		c, _ := geminiTTSTestContext(t)
		info := geminiTTSRelayInfo(geminiTTSFlashModel, format)

		reader, err := adaptor.ConvertAudioRequest(c, info, geminiTTSAudioRequest("alloy", format))
		require.NoError(t, err)

		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		bodies[format] = string(body)
	}

	assert.Equal(t, bodies["pcm"], bodies["wav"])
}

func gjsonStrings(result gjson.Result) []string {
	values := make([]string, 0, len(result.Array()))
	for _, item := range result.Array() {
		values = append(values, item.String())
	}
	return values
}

func TestGeminiConvertAudioRequestRejectsNonSpeechRelayModes(t *testing.T) {
	adaptor := &Adaptor{}
	for _, mode := range []int{relayconstant.RelayModeAudioTranscription, relayconstant.RelayModeAudioTranslation} {
		c, _ := geminiTTSTestContext(t)
		info := geminiTTSRelayInfo(geminiTTSFlashModel, "wav")
		info.RelayMode = mode

		reader, err := adaptor.ConvertAudioRequest(c, info, geminiTTSAudioRequest("alloy", "wav"))
		require.Error(t, err)
		assert.Nil(t, reader)
	}
}

func TestGeminiTTSHandlerReturnsRawPCM(t *testing.T) {
	c, recorder := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")

	usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, geminiTTSAudioUsageMetadata()))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "audio/pcm", recorder.Header().Get("Content-Type"))
	assert.Equal(t, geminiTTSSamplePCM, recorder.Body.Bytes())
}

func TestGeminiTTSHandlerReturnsWav(t *testing.T) {
	c, recorder := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSProModel, "wav")

	usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, geminiTTSAudioUsageMetadata()))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "audio/wav", recorder.Header().Get("Content-Type"))

	body := recorder.Body.Bytes()
	require.Len(t, body, 44+len(geminiTTSSamplePCM))
	assert.Equal(t, geminiTTSSamplePCM, body[44:])
}

func TestPCMToWavBuildsCanonicalRiffHeader(t *testing.T) {
	pcm := bytes.Repeat([]byte{0x12, 0x34}, 8)

	wav, err := pcmToWav(pcm)
	require.NoError(t, err)
	require.Len(t, wav, 44+len(pcm))

	assert.Equal(t, "RIFF", string(wav[0:4]))
	assert.Equal(t, uint32(36+len(pcm)), binary.LittleEndian.Uint32(wav[4:8]))
	assert.Equal(t, "WAVE", string(wav[8:12]))
	assert.Equal(t, "fmt ", string(wav[12:16]))
	assert.Equal(t, uint32(16), binary.LittleEndian.Uint32(wav[16:20]))
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(wav[20:22]))
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(wav[22:24]))
	assert.Equal(t, uint32(24000), binary.LittleEndian.Uint32(wav[24:28]))
	assert.Equal(t, uint32(48000), binary.LittleEndian.Uint32(wav[28:32]))
	assert.Equal(t, uint16(2), binary.LittleEndian.Uint16(wav[32:34]))
	assert.Equal(t, uint16(16), binary.LittleEndian.Uint16(wav[34:36]))
	assert.Equal(t, "data", string(wav[36:40]))
	assert.Equal(t, uint32(len(pcm)), binary.LittleEndian.Uint32(wav[40:44]))
	assert.Equal(t, pcm, wav[44:])
}

func TestPCMToWavRejectsUnalignedPayload(t *testing.T) {
	wav, err := pcmToWav([]byte{0x01, 0x02, 0x03})

	require.Error(t, err)
	assert.Nil(t, wav)
}

func TestGeminiTTSHandlerMimeContract(t *testing.T) {
	cases := []struct {
		name     string
		mimeType string
		wantErr  bool
	}{
		{name: "canonical order", mimeType: "audio/L16;codec=pcm;rate=24000"},
		{name: "reversed parameter order", mimeType: "audio/L16;rate=24000;codec=pcm"},
		{name: "spaced parameters", mimeType: "audio/L16; codec=pcm; rate=24000"},
		{name: "lowercase media type", mimeType: "audio/l16;codec=pcm;rate=24000"},
		{name: "explicit mono channels", mimeType: "audio/L16;codec=pcm;rate=24000;channels=1"},
		{name: "stereo channels", mimeType: "audio/L16;codec=pcm;rate=24000;channels=2", wantErr: true},
		{name: "unsupported codec", mimeType: "audio/L16;codec=opus;rate=24000", wantErr: true},
		{name: "uppercase codec value", mimeType: "audio/L16;codec=PCM;rate=24000", wantErr: true},
		{name: "mixed case codec value", mimeType: "audio/L16;codec=Pcm;rate=24000", wantErr: true},
		{name: "missing codec", mimeType: "audio/L16;rate=24000", wantErr: true},
		{name: "unsupported rate", mimeType: "audio/L16;codec=pcm;rate=48000", wantErr: true},
		{name: "missing rate", mimeType: "audio/L16;codec=pcm", wantErr: true},
		{name: "unsupported media type", mimeType: "audio/mpeg;codec=pcm;rate=24000", wantErr: true},
		{name: "unparsable mime", mimeType: "audio/L16;;;codec", wantErr: true},
		{name: "empty mime", mimeType: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder := geminiTTSTestContext(t)
			info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")

			usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, tc.mimeType, geminiTTSSamplePCM, geminiTTSAudioUsageMetadata()))

			if tc.wantErr {
				require.NotNil(t, apiErr)
				assert.Nil(t, usage)
				assert.Empty(t, recorder.Body.Bytes())
				return
			}

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, geminiTTSSamplePCM, recorder.Body.Bytes())
		})
	}
}

func TestGeminiTTSHandlerRejectsUnusableResponses(t *testing.T) {
	audioData := base64.StdEncoding.EncodeToString(geminiTTSSamplePCM)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   types.ErrorCode
	}{
		{
			name:       "prompt blocked",
			body:       `{"promptFeedback":{"blockReason":"SAFETY"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   types.ErrorCodePromptBlocked,
		},
		{
			name:       "candidate blocked",
			body:       `{"candidates":[{"finishReason":"PROHIBITED_CONTENT","content":{"parts":[]}}]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   types.ErrorCodePromptBlocked,
		},
		{
			name:       "candidate failed",
			body:       `{"candidates":[{"finishReason":"LANGUAGE","content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"` + audioData + `"}}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":200,"totalTokenCount":300}}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeBadResponse,
		},
		{
			name:       "no candidates",
			body:       `{"candidates":[]}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeEmptyResponse,
		},
		{
			name:       "candidate without parts",
			body:       `{"candidates":[{"content":{"parts":[]}}]}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeEmptyResponse,
		},
		{
			name:       "text only parts",
			body:       `{"candidates":[{"content":{"parts":[{"text":"I cannot do that"}]}}]}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeEmptyResponse,
		},
		{
			name:       "empty audio data",
			body:       `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":""}}]}}]}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeEmptyResponse,
		},
		{
			name:       "malformed base64",
			body:       `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"!!!not-base64!!!"}}]}}]}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeBadResponseBody,
		},
		{
			name:       "noncanonical base64 padding",
			body:       `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"Zh=="}}]}}]}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeBadResponseBody,
		},
		{
			name:       "malformed json",
			body:       `{"candidates":`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeBadResponseBody,
		},
		{
			name:       "missing usage metadata",
			body:       `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"` + audioData + `"}}]}}]}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeBadResponse,
		},
		{
			name:       "zero usage metadata",
			body:       `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"` + audioData + `"}}]}}],"usageMetadata":{}}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeBadResponse,
		},
		{
			name:       "text only completion tokens",
			body:       `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"` + audioData + `"}}]}}],"usageMetadata":{"promptTokenCount":100,"totalTokenCount":100}}`,
			wantStatus: http.StatusInternalServerError,
			wantCode:   types.ErrorCodeBadResponse,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder := geminiTTSTestContext(t)
			info := geminiTTSRelayInfo(geminiTTSFlashModel, "wav")

			usage, apiErr := GeminiTTSHandler(c, info, geminiTTSRawResponse(tc.body))

			require.NotNil(t, apiErr)
			assert.Nil(t, usage)
			assert.Equal(t, tc.wantStatus, apiErr.StatusCode)
			assert.Equal(t, tc.wantCode, apiErr.GetErrorCode())
			assert.Empty(t, recorder.Body.Bytes())
		})
	}
}

func TestGeminiTTSHandlerRejectsUnalignedPCM(t *testing.T) {
	for _, responseFormat := range []string{"pcm", "wav"} {
		t.Run(responseFormat, func(t *testing.T) {
			c, recorder := geminiTTSTestContext(t)
			info := geminiTTSRelayInfo(geminiTTSFlashModel, responseFormat)

			usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, []byte{0x01}, geminiTTSAudioUsageMetadata()))

			require.NotNil(t, apiErr)
			assert.Nil(t, usage)
			assert.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
			assert.Empty(t, recorder.Body.Bytes())
		})
	}
}

func TestGeminiTTSHandlerRejectsInconsistentUsage(t *testing.T) {
	cases := []struct {
		name                 string
		estimatePromptTokens int
		metadata             dto.GeminiUsageMetadata
	}{
		{
			name: "candidate details below aggregate",
			metadata: dto.GeminiUsageMetadata{
				PromptTokenCount:        100,
				CandidatesTokenCount:    200,
				TotalTokenCount:         300,
				PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}},
				CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 1}},
			},
		},
		{
			name: "candidate details above aggregate",
			metadata: dto.GeminiUsageMetadata{
				PromptTokenCount:        100,
				CandidatesTokenCount:    200,
				TotalTokenCount:         300,
				PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}},
				CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 201}},
			},
		},
		{
			name: "negative candidate detail",
			metadata: dto.GeminiUsageMetadata{
				PromptTokenCount:        100,
				CandidatesTokenCount:    200,
				TotalTokenCount:         300,
				PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}},
				CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: -1}},
			},
		},
		{
			name: "unexpected prompt modality",
			metadata: dto.GeminiUsageMetadata{
				PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 100}},
				CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 200}},
			},
		},
		{
			name: "negative aggregate",
			metadata: dto.GeminiUsageMetadata{
				PromptTokenCount:     100,
				CandidatesTokenCount: -1,
				TotalTokenCount:      99,
				PromptTokensDetails:  []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}},
			},
		},
		{
			name: "total disagrees with components",
			metadata: dto.GeminiUsageMetadata{
				PromptTokenCount:     100,
				CandidatesTokenCount: 200,
				TotalTokenCount:      299,
			},
		},
		{
			name: "aggregate counters overflow without total",
			metadata: dto.GeminiUsageMetadata{
				PromptTokenCount:     int(^uint(0) >> 1),
				CandidatesTokenCount: 1,
			},
		},
		{
			name: "missing billable text input",
			metadata: dto.GeminiUsageMetadata{
				CandidatesTokenCount: 200,
				TotalTokenCount:      200,
			},
		},
		{
			name:                 "zero prompt detail does not use local estimate",
			estimatePromptTokens: 17,
			metadata: dto.GeminiUsageMetadata{
				PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 0}},
				CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 200}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder := geminiTTSTestContext(t)
			info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")
			info.SetEstimatePromptTokens(tc.estimatePromptTokens)

			usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, tc.metadata))

			require.NotNil(t, apiErr)
			assert.Nil(t, usage)
			assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
			assert.Empty(t, recorder.Body.Bytes())
		})
	}
}

func TestGeminiTTSHandlerMapsModalityUsage(t *testing.T) {
	c, _ := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")

	usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, geminiTTSAudioUsageMetadata()))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.PromptTokens)
	assert.Equal(t, 100, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 0, usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 200, usage.CompletionTokenDetails.AudioTokens)
	assert.Equal(t, 0, usage.CompletionTokenDetails.TextTokens)
	assert.Equal(t, 300, usage.TotalTokens)
}

func TestGeminiTTSHandlerNormalizesMissingTotalTokens(t *testing.T) {
	c, recorder := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")

	metadata := dto.GeminiUsageMetadata{
		PromptTokenCount:        100,
		CandidatesTokenCount:    200,
		TotalTokenCount:         0,
		PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 200}},
	}

	usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, metadata))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.PromptTokens)
	assert.Equal(t, 100, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 200, usage.CompletionTokens)
	assert.Equal(t, 200, usage.CompletionTokenDetails.AudioTokens)
	assert.Equal(t, 300, usage.TotalTokens)
	assert.Equal(t, geminiTTSSamplePCM, recorder.Body.Bytes())
}

func TestGeminiTTSHandlerRejectsUnexpectedTextCandidateDetails(t *testing.T) {
	c, recorder := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")

	metadata := dto.GeminiUsageMetadata{
		PromptTokenCount:        100,
		CandidatesTokenCount:    200,
		TotalTokenCount:         300,
		PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 200}},
	}

	usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, metadata))

	require.NotNil(t, apiErr)
	assert.Nil(t, usage)
	assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.Empty(t, recorder.Body.Bytes())
}

func TestGeminiTTSHandlerPrefersPromptDetailsOverLocalEstimate(t *testing.T) {
	c, recorder := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")
	info.SetEstimatePromptTokens(17)
	metadata := dto.GeminiUsageMetadata{
		TotalTokenCount:         300,
		PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 200}},
	}

	usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, metadata))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.PromptTokens)
	assert.Equal(t, 100, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 200, usage.CompletionTokens)
	assert.Equal(t, 300, usage.TotalTokens)
	assert.Equal(t, geminiTTSSamplePCM, recorder.Body.Bytes())
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

func TestGeminiTTSHandlerUsesLocalEstimateWhenPromptUsageIsMissing(t *testing.T) {
	c, recorder := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "pcm")
	info.SetEstimatePromptTokens(17)
	metadata := dto.GeminiUsageMetadata{
		TotalTokenCount:         200,
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 200}},
	}

	usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, metadata))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 17, usage.PromptTokens)
	assert.Equal(t, 17, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 200, usage.CompletionTokens)
	assert.Equal(t, 217, usage.TotalTokens)
	assert.Equal(t, geminiTTSSamplePCM, recorder.Body.Bytes())
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

func TestGeminiTTSHandlerAggregateFallbackIsLimitedToTTSModels(t *testing.T) {
	aggregateOnly := dto.GeminiUsageMetadata{
		PromptTokenCount:     100,
		CandidatesTokenCount: 200,
		TotalTokenCount:      300,
	}

	for _, model := range []string{geminiTTSFlashModel, geminiTTSProModel} {
		t.Run("fallback applies to "+model, func(t *testing.T) {
			c, recorder := geminiTTSTestContext(t)
			info := geminiTTSRelayInfo(model, "pcm")

			usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, aggregateOnly))

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 200, usage.CompletionTokenDetails.AudioTokens)
			assert.Equal(t, 100, usage.PromptTokensDetails.TextTokens)
			assert.Equal(t, geminiTTSSamplePCM, recorder.Body.Bytes())
		})
	}

	for _, model := range []string{"gemini-2.5-flash", "gemini-2.5-pro", "gemini-2.5-flash-native-audio-latest"} {
		t.Run("fallback rejected for "+model, func(t *testing.T) {
			c, recorder := geminiTTSTestContext(t)
			info := geminiTTSRelayInfo(model, "pcm")

			usage, apiErr := GeminiTTSHandler(c, info, geminiTTSResponse(t, geminiTTSMimeL16, geminiTTSSamplePCM, aggregateOnly))

			require.NotNil(t, apiErr)
			assert.Nil(t, usage)
			assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
			assert.Empty(t, recorder.Body.Bytes())
		})
	}
}

func TestGeminiChatHandlerKeepsAggregateCandidateTokensAsTextCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: geminiTTSFlashModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: geminiTTSFlashModel},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "ok"}}}},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     100,
			CandidatesTokenCount: 200,
			TotalTokenCount:      300,
		},
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, apiErr := GeminiChatHandler(c, info, &http.Response{Body: io.NopCloser(bytes.NewReader(body))})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 200, usage.CompletionTokens)
	assert.Equal(t, 0, usage.CompletionTokenDetails.AudioTokens)
}

func TestGeminiTTSUpstreamRequestContract(t *testing.T) {
	service.InitHttpClient()

	upstreamCalls := 0
	var gotPath, gotAPIKey string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-goog-api-key")
		gotBody, _ = io.ReadAll(r.Body)

		payload := dto.GeminiChatResponse{
			Candidates: []dto.GeminiChatCandidate{
				{
					Content: dto.GeminiChatContent{
						Role: "model",
						Parts: []dto.GeminiPart{
							{InlineData: &dto.GeminiInlineData{
								MimeType: geminiTTSMimeL16,
								Data:     base64.StdEncoding.EncodeToString(geminiTTSSamplePCM),
							}},
						},
					},
				},
			},
			UsageMetadata: geminiTTSAudioUsageMetadata(),
		}
		responseBody, err := common.Marshal(payload)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(responseBody)
	}))
	defer server.Close()

	c, recorder := geminiTTSTestContext(t)
	info := geminiTTSRelayInfo(geminiTTSFlashModel, "wav")
	info.ChannelMeta.ChannelBaseUrl = server.URL
	info.ChannelMeta.ApiKey = "test-gemini-key"

	adaptor := &Adaptor{}
	adaptor.Init(info)

	reader, err := adaptor.ConvertAudioRequest(c, info, geminiTTSAudioRequest("nova", "wav"))
	require.NoError(t, err)

	resp, err := adaptor.DoRequest(c, info, reader)
	require.NoError(t, err)

	httpResp, ok := resp.(*http.Response)
	require.True(t, ok)
	require.Equal(t, http.StatusOK, httpResp.StatusCode)

	usage, apiErr := adaptor.DoResponse(c, httpResp, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)

	assert.Equal(t, 1, upstreamCalls)
	assert.Equal(t, "/v1beta/models/gemini-2.5-flash-preview-tts:generateContent", gotPath)
	assert.Equal(t, "test-gemini-key", gotAPIKey)
	assert.Equal(t, "hello world", gjson.GetBytes(gotBody, "contents.0.parts.0.text").String())
	assert.Equal(t, "Aoede", gjson.GetBytes(gotBody, "generationConfig.speechConfig.voiceConfig.prebuiltVoiceConfig.voiceName").String())
	assert.Equal(t, []string{"AUDIO"}, gjsonStrings(gjson.GetBytes(gotBody, "generationConfig.responseModalities")))

	body := recorder.Body.Bytes()
	assert.Equal(t, "audio/wav", recorder.Header().Get("Content-Type"))
	require.Len(t, body, 44+len(geminiTTSSamplePCM))
	assert.Equal(t, "RIFF", string(body[0:4]))
	assert.Equal(t, geminiTTSSamplePCM, body[44:])
	assert.Equal(t, 200, usage.(*dto.Usage).CompletionTokenDetails.AudioTokens)
}
