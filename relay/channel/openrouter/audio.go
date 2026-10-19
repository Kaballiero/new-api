package openrouter

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/provider"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// transcriptionReservationPayload describes the outgoing multipart order without
// counting file bytes as text tokens. Expressions see scalar form fields and a
// file descriptor (filename/size), never invented audio seconds or tokens.
func transcriptionReservationPayload(contentType string, body []byte) ([]byte, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, errors.New("OpenRouter transcription requires multipart/form-data")
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	fields := make(map[string]any)
	fileSeen := false
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("invalid OpenRouter transcription multipart body")
		}
		name := part.FormName()
		if name == "file" {
			if fileSeen || part.FileName() == "" {
				return nil, errors.New("OpenRouter transcription requires exactly one audio file")
			}
			size, err := io.Copy(io.Discard, io.LimitReader(part, (25<<20)+1))
			if err != nil || size == 0 || size > 25<<20 {
				return nil, errors.New("OpenRouter transcription file must contain at most 25 MiB")
			}
			fields["file"] = map[string]any{"filename": part.FileName(), "size": size}
			fileSeen = true
			continue
		}
		switch name {
		case "model", "language", "temperature", "response_format", "prompt", "timestamp_granularities[]", "timestamp_granularities", "provider":
		default:
			return nil, errors.New("unsupported OpenRouter transcription multipart field")
		}
		value, err := io.ReadAll(io.LimitReader(part, (64<<10)+1))
		if err != nil || len(value) > 64<<10 {
			return nil, errors.New("OpenRouter transcription field exceeds inspection limit")
		}
		if name == "timestamp_granularities[]" || name == "timestamp_granularities" {
			values, _ := fields["timestamp_granularities"].([]string)
			fields["timestamp_granularities"] = append(values, string(value))
			continue
		}
		if _, exists := fields[name]; exists {
			return nil, errors.New("duplicate OpenRouter transcription field")
		}
		if name == "provider" {
			if err = provider.Validate("audio/transcriptions", value); err != nil {
				return nil, err
			}
			var options any
			if err = common.Unmarshal(value, &options); err != nil {
				return nil, err
			}
			fields[name] = options
			continue
		}
		fields[name] = string(value)
	}
	if !fileSeen {
		return nil, errors.New("OpenRouter transcription requires exactly one audio file")
	}
	return common.Marshal(fields)
}

// AudioResponse persists the complete result before delivery. An unknown cost
// retains the durable Task reservation for metadata reconciliation.
func AudioResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage *dto.Usage, apiErr *types.NewAPIError) {
	defer func() {
		if apiErr != nil && service.HasOpenRouterSyncTask(info) {
			_ = service.FailOpenRouterSyncTask(info, "invalid_audio_result")
		}
	}()
	if resp == nil || resp.Body == nil {
		return nil, CostError("OpenRouter audio response is missing")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	if err != nil || len(body) == 0 || len(body) > 64<<20 {
		return nil, CostError("OpenRouter audio response cannot be inspected for authoritative cost")
	}
	state := info.OpenRouterCost
	resultType := "audio"
	mimeType := resp.Header.Get("Content-Type")
	if state.BufferedAudio {
		mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil || mediaType != "text/event-stream" {
			return nil, CostError("OpenRouter Chat audio requires an SSE response")
		}
		resultType = "file"
		var last []byte
		var projected bytes.Buffer
		done := false
		// OpenRouter Chat SSE uses one JSON data line per event. Reject trailing
		// events and malformed terminal usage instead of retaining provisional cost.
		for line := range bytes.SplitSeq(body, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 || line[0] == ':' {
				continue
			}
			data, ok := bytes.CutPrefix(line, []byte("data:"))
			if !ok {
				continue
			}
			data = bytes.TrimSpace(data)
			if done {
				return nil, CostError("OpenRouter audio has events after terminal marker")
			}
			if bytes.Equal(data, []byte("[DONE]")) {
				state.Observe(last, true)
				done = true
				projected.WriteString("data: [DONE]\n\n")
				continue
			}
			var event struct {
				ID string `json:"id"`
			}
			if common.Unmarshal(data, &event) == nil && state.GenerationID == "" {
				state.GenerationID = event.ID
			}
			state.Observe(data, false)
			// Do not allow a later valid event to erase an upstream error.
			if state.UnknownReason == "malformed_usage" || state.UnknownReason == "upstream_error" {
				return nil, CostError("OpenRouter audio stream failed")
			}
			clientData, err := common.ProjectClientResponse(c, data)
			if err != nil {
				return nil, CostError("OpenRouter audio response projection failed")
			}
			projected.WriteString("data: ")
			projected.Write(clientData)
			projected.WriteString("\n\n")
			last = data
		}
		if !done || len(last) == 0 {
			return nil, CostError("OpenRouter audio terminal marker is missing")
		}
		body = projected.Bytes()
	} else if info.RelayMode == relayconstant.RelayModeAudioTranscription {
		resultType = "file"
		mediaType, _, parseErr := mime.ParseMediaType(mimeType)
		if parseErr != nil || mediaType != "application/json" {
			return nil, CostError("OpenRouter transcription response is not JSON")
		}
		var result struct {
			ID   string  `json:"id"`
			Text *string `json:"text"`
		}
		if common.Unmarshal(body, &result) != nil || result.Text == nil {
			return nil, CostError("OpenRouter transcription response is invalid")
		}
		state.Observe(body, true)
		if state.UnknownReason == "malformed_usage" || state.UnknownReason == "upstream_error" {
			return nil, CostError("OpenRouter transcription failed")
		}
		if state.GenerationID == "" {
			state.GenerationID = result.ID
		}
		body, err = common.ProjectClientResponse(c, body)
		if err != nil {
			return nil, CostError("OpenRouter transcription response projection failed")
		}
	} else {
		mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "audio/") {
			return nil, CostError("OpenRouter speech response is not audio")
		}
	}
	if err := service.CompleteOpenRouterSyncTask(c, info, body, mimeType, resultType); err != nil {
		return nil, CostError(err.Error())
	}
	c.Header("X-Task-Id", state.TaskID)
	// Preserve the gateway's existing response/header policy while
	// retaining audio fields even if ForceFormat/ThinkingToContent are enabled.
	service.IOCopyOpaqueBytesGracefully(c, resp, body)
	return &dto.Usage{Cost: state.CostUSD}, nil
}
