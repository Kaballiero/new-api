package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	geminiTTSFormatPCM = "pcm"
	geminiTTSFormatWAV = "wav"

	geminiTTSSampleRate = 24000
	geminiTTSChannels   = 1
	geminiTTSBitDepth   = 16

	geminiTTSMediaType = "audio/l16"
	geminiTTSCodec     = "pcm"
)

// Native Gemini voice names are intentionally excluded from the OpenAI-compatible endpoint.
var openAIToGeminiVoiceMap = map[string]string{
	"alloy":   "Kore",
	"echo":    "Charon",
	"fable":   "Puck",
	"onyx":    "Gacrux",
	"nova":    "Aoede",
	"shimmer": "Leda",
}

// Restricting aggregate fallback prevents non-TTS candidate tokens from being billed as audio.
var geminiTTSSupportedModels = map[string]bool{
	"gemini-2.5-flash-preview-tts": true,
	"gemini-2.5-pro-preview-tts":   true,
}

var geminiTTSBlockingFinishReasons = map[string]bool{
	"SAFETY":             true,
	"RECITATION":         true,
	"BLOCKLIST":          true,
	"PROHIBITED_CONTENT": true,
	"SPII":               true,
}

type geminiTTSPrebuiltVoiceConfig struct {
	VoiceName string `json:"voiceName"`
}

type geminiTTSVoiceConfig struct {
	PrebuiltVoiceConfig geminiTTSPrebuiltVoiceConfig `json:"prebuiltVoiceConfig"`
}

type geminiTTSSpeechConfig struct {
	VoiceConfig geminiTTSVoiceConfig `json:"voiceConfig"`
}

func geminiTTSBadRequest(message string) error {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
}

func geminiTTSMaxResponseBytes() (int64, error) {
	const bytesPerMB int64 = 1 << 20
	maxResponseMB := int64(constant.MaxFileDownloadMB)
	if maxResponseMB <= 0 || maxResponseMB > (math.MaxInt64-1)/bytesPerMB {
		return 0, errors.New("invalid MAX_FILE_DOWNLOAD_MB configuration")
	}
	return maxResponseMB * bytesPerMB, nil
}

// Omitted response_format implies mp3 in OpenAI, which this PCM-only path cannot produce.
func geminiTTSAudioFormat(request *dto.AudioRequest) (string, bool) {
	switch request.ResponseFormat {
	case geminiTTSFormatPCM, geminiTTSFormatWAV:
		return request.ResponseFormat, true
	}
	return "", false
}

func convertGeminiTTSRequest(c *gin.Context, request dto.AudioRequest) (io.Reader, error) {
	if !geminiTTSSupportedModels[request.Model] {
		return nil, geminiTTSBadRequest(fmt.Sprintf(
			"unsupported model %q for gemini text-to-speech, supported models: gemini-2.5-flash-preview-tts, gemini-2.5-pro-preview-tts",
			request.Model))
	}

	if strings.TrimSpace(request.Input) == "" {
		return nil, geminiTTSBadRequest("input is required for gemini text-to-speech")
	}

	voiceName, ok := openAIToGeminiVoiceMap[request.Voice]
	if !ok {
		return nil, geminiTTSBadRequest(fmt.Sprintf(
			"unsupported voice %q for gemini text-to-speech, supported voices: alloy, echo, fable, onyx, nova, shimmer",
			request.Voice))
	}

	if _, ok := geminiTTSAudioFormat(&request); !ok {
		return nil, geminiTTSBadRequest(fmt.Sprintf(
			"unsupported response_format %q for gemini text-to-speech, response_format must be explicitly set to pcm or wav",
			request.ResponseFormat))
	}

	if request.Speed != nil && *request.Speed != 1 {
		return nil, geminiTTSBadRequest(fmt.Sprintf("unsupported speed %v for gemini text-to-speech, only speed 1 is supported", *request.Speed))
	}

	if request.IsStream(c) {
		return nil, geminiTTSBadRequest("streaming is not supported for gemini text-to-speech")
	}

	if request.Instructions != "" {
		return nil, geminiTTSBadRequest("instructions are not supported for gemini text-to-speech")
	}
	if _, err := geminiTTSMaxResponseBytes(); err != nil {
		return nil, err
	}

	speechConfig, err := common.Marshal(geminiTTSSpeechConfig{
		VoiceConfig: geminiTTSVoiceConfig{
			PrebuiltVoiceConfig: geminiTTSPrebuiltVoiceConfig{VoiceName: voiceName},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gemini speech config: %w", err)
	}

	geminiRequest := dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role:  "user",
				Parts: []dto.GeminiPart{{Text: request.Input}},
			},
		},
		GenerationConfig: dto.GeminiChatGenerationConfig{
			ResponseModalities: []string{"AUDIO"},
			SpeechConfig:       speechConfig,
		},
	}
	for _, category := range SafetySettingList {
		geminiRequest.SafetySettings = append(geminiRequest.SafetySettings, dto.GeminiChatSafetySettings{
			Category:  category,
			Threshold: model_setting.GetGeminiSafetySetting(category),
		})
	}

	body, err := common.Marshal(geminiRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gemini text-to-speech request: %w", err)
	}
	return bytes.NewReader(body), nil
}

// GeminiTTSHandler validates the full response before writing audio to the client.
func GeminiTTSHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(errors.New("empty response from Gemini API"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	audioRequest, ok := info.Request.(*dto.AudioRequest)
	if !ok {
		return nil, types.NewOpenAIError(
			errors.New("gemini text-to-speech response without an audio request"),
			types.ErrorCodeInvalidRequest,
			http.StatusInternalServerError,
			types.ErrOptionWithSkipRetry(),
		)
	}
	responseFormat, ok := geminiTTSAudioFormat(audioRequest)
	if !ok {
		return nil, types.NewOpenAIError(
			errors.New("gemini text-to-speech response without a supported response_format"),
			types.ErrorCodeInvalidRequest,
			http.StatusInternalServerError,
			types.ErrOptionWithSkipRetry(),
		)
	}

	maxResponseBytes, err := geminiTTSMaxResponseBytes()
	if err != nil {
		return nil, types.NewOpenAIError(
			err,
			types.ErrorCodeBadResponse,
			http.StatusInternalServerError,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if resp.ContentLength > maxResponseBytes {
		return nil, types.NewOpenAIError(
			fmt.Errorf("gemini text-to-speech response size %d exceeds the %d byte limit", resp.ContentLength, maxResponseBytes),
			types.ErrorCodeBadResponseBody,
			http.StatusInternalServerError,
		)
	}

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if int64(len(responseBody)) > maxResponseBytes {
		return nil, types.NewOpenAIError(
			fmt.Errorf("gemini text-to-speech response exceeds the %d byte limit", maxResponseBytes),
			types.ErrorCodeBadResponseBody,
			http.StatusInternalServerError,
		)
	}

	var geminiResponse dto.GeminiChatResponse
	if err := common.Unmarshal(responseBody, &geminiResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
		return nil, types.NewOpenAIError(
			errors.New("request blocked by Gemini API: "+*geminiResponse.PromptFeedback.BlockReason),
			types.ErrorCodePromptBlocked,
			http.StatusBadRequest,
		)
	}
	if len(geminiResponse.Candidates) == 0 {
		return nil, types.NewOpenAIError(errors.New("empty response from Gemini API"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}

	var inlineData *dto.GeminiInlineData
	for _, candidate := range geminiResponse.Candidates {
		if candidate.FinishReason != nil && *candidate.FinishReason != "" && *candidate.FinishReason != "STOP" {
			finishReason := *candidate.FinishReason
			if geminiTTSBlockingFinishReasons[finishReason] {
				return nil, types.NewOpenAIError(
					errors.New("response blocked by Gemini API: "+finishReason),
					types.ErrorCodePromptBlocked,
					http.StatusBadRequest,
				)
			}
			return nil, types.NewOpenAIError(
				errors.New("gemini text-to-speech response failed: "+finishReason),
				types.ErrorCodeBadResponse,
				http.StatusInternalServerError,
			)
		}
		for i := range candidate.Content.Parts {
			if part := &candidate.Content.Parts[i]; part.InlineData != nil {
				inlineData = part.InlineData
				break
			}
		}
		if inlineData != nil {
			break
		}
	}
	if inlineData == nil {
		return nil, types.NewOpenAIError(errors.New("gemini text-to-speech response contains no audio part"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}
	if inlineData.Data == "" {
		return nil, types.NewOpenAIError(errors.New("gemini text-to-speech response contains empty audio data"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}

	if err := validateGeminiTTSMimeType(inlineData.MimeType); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	pcm, err := base64.StdEncoding.Strict().DecodeString(inlineData.Data)
	if err != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("failed to decode gemini audio data: %w", err), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if len(pcm) == 0 {
		return nil, types.NewOpenAIError(errors.New("gemini text-to-speech response contains empty audio data"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}
	payload := pcm
	contentType := "audio/pcm"
	if responseFormat == geminiTTSFormatWAV {
		payload, err = pcmToWav(pcm)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		contentType = "audio/wav"
	} else if len(pcm)%(geminiTTSBitDepth/8) != 0 {
		return nil, types.NewOpenAIError(
			fmt.Errorf("gemini audio payload of %d bytes is not %d-bit aligned", len(pcm), geminiTTSBitDepth),
			types.ErrorCodeBadResponseBody,
			http.StatusInternalServerError,
		)
	}

	metadata := geminiResponse.GetUsageMetadata()
	if err := validateGeminiTTSUsageMetadata(metadata); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	estimatePromptTokens := info.GetEstimatePromptTokens()
	usedLocalPromptEstimate := metadata.PromptTokenCount == 0 && len(metadata.PromptTokensDetails) == 0 && estimatePromptTokens > 0
	usage := relayconvert.UsageFromGeminiMetadata(metadata, estimatePromptTokens)
	if usage.CompletionTokenDetails.AudioTokens <= 0 {
		// Gemini TTS may omit audio details while reporting aggregate candidate tokens.
		if !geminiTTSSupportedModels[info.UpstreamModelName] || metadata.CandidatesTokenCount <= 0 {
			return nil, types.NewOpenAIError(
				errors.New("gemini text-to-speech response has no billable audio output tokens"),
				types.ErrorCodeBadResponse,
				http.StatusInternalServerError,
			)
		}
		usage.CompletionTokenDetails.AudioTokens = metadata.CandidatesTokenCount
	}
	if usage.PromptTokensDetails.TextTokens <= 0 {
		return nil, types.NewOpenAIError(
			errors.New("gemini text-to-speech response has no billable text input tokens"),
			types.ErrorCodeBadResponse,
			http.StatusInternalServerError,
		)
	}

	// Prefer provider details over a local estimate so aggregate logs match billed tokens.
	if metadata.PromptTokenCount == 0 && len(metadata.PromptTokensDetails) > 0 {
		usage.PromptTokens = usage.PromptTokensDetails.TextTokens
	}
	usage.CompletionTokens = usage.CompletionTokenDetails.TextTokens +
		usage.CompletionTokenDetails.AudioTokens +
		usage.CompletionTokenDetails.ImageTokens +
		usage.CompletionTokenDetails.ReasoningTokens

	maxInt := math.MaxInt
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.CompletionTokens > maxInt-usage.PromptTokens {
		return nil, types.NewOpenAIError(
			errors.New("gemini text-to-speech token totals overflow"),
			types.ErrorCodeBadResponse,
			http.StatusInternalServerError,
		)
	}

	// PostAudioConsumeQuota uses this aggregate as its success and settlement gate.
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	if usedLocalPromptEstimate {
		common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	}
	c.Data(http.StatusOK, contentType, payload)
	return usage, nil
}

func validateGeminiTTSUsageMetadata(metadata *dto.GeminiUsageMetadata) error {
	if metadata == nil {
		return errors.New("gemini text-to-speech response has no usage metadata")
	}

	aggregates := []struct {
		name  string
		value int
	}{
		{name: "promptTokenCount", value: metadata.PromptTokenCount},
		{name: "toolUsePromptTokenCount", value: metadata.ToolUsePromptTokenCount},
		{name: "candidatesTokenCount", value: metadata.CandidatesTokenCount},
		{name: "totalTokenCount", value: metadata.TotalTokenCount},
		{name: "thoughtsTokenCount", value: metadata.ThoughtsTokenCount},
		{name: "cachedContentTokenCount", value: metadata.CachedContentTokenCount},
	}
	for _, aggregate := range aggregates {
		if aggregate.value < 0 {
			return fmt.Errorf("gemini text-to-speech response has negative %s", aggregate.name)
		}
	}
	if metadata.ToolUsePromptTokenCount != 0 || len(metadata.ToolUsePromptTokensDetails) != 0 {
		return errors.New("gemini text-to-speech response contains unexpected tool usage tokens")
	}
	if metadata.ThoughtsTokenCount != 0 {
		return errors.New("gemini text-to-speech response contains unexpected reasoning tokens")
	}
	if metadata.PromptTokenCount > 0 && metadata.CachedContentTokenCount > metadata.PromptTokenCount {
		return errors.New("gemini text-to-speech cached token count exceeds prompt token count")
	}

	promptDetailTokens, err := sumGeminiTTSTokenDetails(metadata.PromptTokensDetails, "TEXT", "prompt")
	if err != nil {
		return err
	}
	candidateDetailTokens, err := sumGeminiTTSTokenDetails(metadata.CandidatesTokensDetails, "AUDIO", "candidate")
	if err != nil {
		return err
	}
	if len(metadata.PromptTokensDetails) > 0 && metadata.PromptTokenCount > 0 && promptDetailTokens != metadata.PromptTokenCount {
		return fmt.Errorf("gemini text-to-speech prompt token details total %d does not match promptTokenCount %d", promptDetailTokens, metadata.PromptTokenCount)
	}
	if len(metadata.CandidatesTokensDetails) > 0 && metadata.CandidatesTokenCount > 0 && candidateDetailTokens != metadata.CandidatesTokenCount {
		return fmt.Errorf("gemini text-to-speech candidate token details total %d does not match candidatesTokenCount %d", candidateDetailTokens, metadata.CandidatesTokenCount)
	}
	if len(metadata.PromptTokensDetails) > 0 && promptDetailTokens == 0 {
		return errors.New("gemini text-to-speech response has no billable text input tokens")
	}

	promptTokens := metadata.PromptTokenCount
	if promptTokens == 0 {
		promptTokens = promptDetailTokens
	}
	candidateTokens := metadata.CandidatesTokenCount
	if candidateTokens == 0 {
		candidateTokens = candidateDetailTokens
	}
	maxInt := math.MaxInt
	if candidateTokens > maxInt-promptTokens {
		return errors.New("gemini text-to-speech prompt and candidate token counts overflow")
	}
	if metadata.TotalTokenCount > 0 && promptTokens+candidateTokens != metadata.TotalTokenCount {
		return fmt.Errorf("gemini text-to-speech totalTokenCount %d does not match prompt and candidate token counts", metadata.TotalTokenCount)
	}

	return nil
}

func sumGeminiTTSTokenDetails(details []dto.GeminiPromptTokensDetails, expectedModality, name string) (int, error) {
	total := 0
	maxInt := math.MaxInt
	for _, detail := range details {
		if detail.TokenCount < 0 {
			return 0, fmt.Errorf("gemini text-to-speech response has negative %s token details", name)
		}
		if detail.Modality != expectedModality {
			return 0, fmt.Errorf("gemini text-to-speech response has unexpected %s modality %q", name, detail.Modality)
		}
		if detail.TokenCount > maxInt-total {
			return 0, fmt.Errorf("gemini text-to-speech %s token details overflow", name)
		}
		total += detail.TokenCount
	}
	return total, nil
}

// MIME parameters are unordered, so parse them instead of matching the raw string.
func validateGeminiTTSMimeType(mimeType string) error {
	mediaType, params, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return fmt.Errorf("unexpected gemini audio mime type %q: %w", mimeType, err)
	}
	if mediaType != geminiTTSMediaType {
		return fmt.Errorf("unexpected gemini audio media type %q, expected %s", mediaType, geminiTTSMediaType)
	}
	if params["codec"] != geminiTTSCodec {
		return fmt.Errorf("unexpected gemini audio codec %q, expected %s", params["codec"], geminiTTSCodec)
	}
	if params["rate"] != fmt.Sprintf("%d", geminiTTSSampleRate) {
		return fmt.Errorf("unexpected gemini audio rate %q, expected %d", params["rate"], geminiTTSSampleRate)
	}
	// A missing channels parameter means mono in the Gemini TTS contract.
	if channels, ok := params["channels"]; ok && channels != fmt.Sprintf("%d", geminiTTSChannels) {
		return fmt.Errorf("unexpected gemini audio channels %q, expected %d", channels, geminiTTSChannels)
	}
	return nil
}

// pcmToWav rejects partial samples and payloads that exceed RIFF's uint32 size fields.
func pcmToWav(pcm []byte) ([]byte, error) {
	const (
		headerSize         = 44
		riffHeaderOverhead = 8 // "RIFF" plus the size field itself are excluded from the RIFF chunk size
		fmtChunkSize       = 16
		pcmFormatCode      = 1
		bytesPerSample     = geminiTTSBitDepth / 8
		blockAlign         = geminiTTSChannels * bytesPerSample
		byteRate           = geminiTTSSampleRate * blockAlign
	)

	if len(pcm)%bytesPerSample != 0 {
		return nil, fmt.Errorf("gemini audio payload of %d bytes is not 16-bit aligned", len(pcm))
	}
	if uint64(len(pcm)) > uint64(math.MaxUint32)-uint64(headerSize-riffHeaderOverhead) {
		return nil, fmt.Errorf("gemini audio payload of %d bytes exceeds the RIFF size limit", len(pcm))
	}

	out := make([]byte, headerSize+len(pcm))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(uint64(headerSize-riffHeaderOverhead)+uint64(len(pcm))))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], fmtChunkSize)
	binary.LittleEndian.PutUint16(out[20:22], pcmFormatCode)
	binary.LittleEndian.PutUint16(out[22:24], geminiTTSChannels)
	binary.LittleEndian.PutUint32(out[24:28], geminiTTSSampleRate)
	binary.LittleEndian.PutUint32(out[28:32], byteRate)
	binary.LittleEndian.PutUint16(out[32:34], blockAlign)
	binary.LittleEndian.PutUint16(out[34:36], geminiTTSBitDepth)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(pcm)))
	copy(out[headerSize:], pcm)
	return out, nil
}
