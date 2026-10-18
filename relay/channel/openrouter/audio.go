package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
		case "model", "language", "temperature", "response_format", "prompt", "timestamp_granularities[]", "timestamp_granularities":
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
		fields[name] = string(value)
	}
	if !fileSeen {
		return nil, errors.New("OpenRouter transcription requires exactly one audio file")
	}
	return common.Marshal(fields)
}

// AudioResponse buffers audio until its authoritative cost is known. Chat keeps
// the upstream SSE bytes, including delta.audio and [DONE]; TTS keeps raw bytes.
// Neither path can publish a successful result with an unknown monetary outcome.
func AudioResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, adaptor channel.Adaptor) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, CostError("OpenRouter audio response is missing")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	if err != nil || len(body) == 0 || len(body) > 64<<20 {
		return nil, CostError("OpenRouter audio response cannot be inspected for authoritative cost")
	}
	state := info.OpenRouterCost
	if state.BufferedAudio {
		mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil || mediaType != "text/event-stream" {
			return nil, CostError("OpenRouter Chat audio requires an SSE response")
		}
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
		if !done || !state.Final {
			return nil, CostError("OpenRouter terminal usage.cost is missing or invalid")
		}
		body = projected.Bytes()
	} else {
		mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "audio/") {
			return nil, CostError("OpenRouter speech response is not audio")
		}
		if err := confirmSpeechCost(c, resp.Header.Get("X-Generation-Id"), info, adaptor); err != nil {
			return nil, CostError(err.Error())
		}
	}
	// Preserve the gateway's existing response/header policy while
	// retaining audio fields even if ForceFormat/ThinkingToContent are enabled.
	service.IOCopyOpaqueBytesGracefully(c, resp, body)
	return &dto.Usage{Cost: state.CostUSD}, nil
}

// confirmSpeechCost performs only a metadata GET after EOF, never a paid replay.
// A missing/not-yet-published record fails closed under the synchronous refund
// policy. Matching identity and completed timing prevent provisional free cost.
func confirmSpeechCost(c *gin.Context, generationID string, info *relaycommon.RelayInfo, adaptor channel.Adaptor) error {
	state := info.OpenRouterCost
	state.UnknownReason = "missing_speech_generation_cost"
	if generationID == "" || len(generationID) > 512 {
		return errors.New("OpenRouter speech generation ID is missing or invalid")
	}
	state.GenerationID = generationID
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(info.ChannelBaseUrl, "/") + "/v1/generation?id=" + url.QueryEscape(generationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("OpenRouter speech cost lookup request failed")
	}
	if err := adaptor.SetupRequestHeader(c, &req.Header, info); err != nil {
		return err
	}
	overrides, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return err
	}
	for key, value := range overrides {
		req.Header.Set(key, value)
	}
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return errors.New("OpenRouter speech cost transport unavailable")
	}
	metadataClient := *client
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := metadataClient.Do(req)
	if err != nil {
		return errors.New("OpenRouter speech cost lookup failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("OpenRouter speech cost lookup unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return errors.New("OpenRouter speech cost metadata exceeds limit")
	}
	var result struct {
		Data struct {
			ID             string          `json:"id"`
			Model          string          `json:"model"`
			Cancelled      *bool           `json:"cancelled"`
			GenerationTime *float64        `json:"generation_time"`
			TotalCost      json.RawMessage `json:"total_cost"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if common.Unmarshal(body, &result) != nil || len(result.Error) > 0 && string(result.Error) != "null" || result.Data.ID != generationID || result.Data.Model != strings.TrimSuffix(strings.TrimSuffix(info.UpstreamModelName, ":nitro"), ":floor") || result.Data.Cancelled == nil || *result.Data.Cancelled || result.Data.GenerationTime == nil || *result.Data.GenerationTime < 0 {
		return errors.New("OpenRouter speech generation cost is incomplete or mismatched")
	}
	cost, err := common.ValidateOpenRouterCost(result.Data.TotalCost)
	if err != nil {
		return errors.New("OpenRouter speech total_cost is missing or invalid")
	}
	state.CostUSD, state.Valid, state.Final, state.UnknownReason = cost, true, true, ""
	state.CostSource = "generation.total_cost"
	return nil
}
