package openrouter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// ValidateGeminiRequest admits only the native Gemini generateContent subset
// that converts to the priced OpenRouter Chat payload without loss, and
// rejects everything else with a message naming the field, before reservation.
//
// Accepted: contents with text, thought text, image/* inlineData or fileData,
// and functionCall or functionResponse parts (a functionCall part only in a
// content without text or media; functionResponse without parts);
// systemInstruction with text parts only (checked on the raw body); tools with
// functionDeclarations only, each an object with a non-empty name and optional
// description and parameters;
// generationConfig temperature, topP, topK, maxOutputTokens, candidateCount
// (1..dto.MaxImageN), at most four stopSequences, responseMimeType
// text/plain, responseModalities TEXT and thinkingConfig. safetySettings (and
// snake_case safety_settings) are accepted and ignored, as OpenRouter Chat has
// no equivalent. Every part carries exactly one of these payloads.
//
// The cached original body is inspected for top-level snake_case keys the DTO
// does not parse (generation_config, tool_config, cached_content), so they are
// rejected instead of being silently lost.
func ValidateGeminiRequest(c *gin.Context, request *dto.GeminiChatRequest) error {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return fmt.Errorf("Gemini request body is unavailable for validation: %w", err)
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("Gemini request body is unavailable for validation: %w", err)
	}
	var raw struct {
		GenerationConfig       json.RawMessage `json:"generation_config"`
		ToolConfig             json.RawMessage `json:"tool_config"`
		CachedContent          json.RawMessage `json:"cached_content"`
		SystemInstruction      json.RawMessage `json:"systemInstruction"`
		SystemInstructionSnake json.RawMessage `json:"system_instruction"`
	}
	decodeErr := common.DecodeJson(storage, &raw)
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("Gemini request body is unavailable for validation: %w", err)
	}
	if decodeErr != nil {
		return errors.New("invalid Gemini request body")
	}
	for _, key := range []struct {
		name, camel string
		raw         json.RawMessage
	}{
		{"generation_config", "generationConfig", raw.GenerationConfig},
		{"tool_config", "toolConfig", raw.ToolConfig},
		{"cached_content", "cachedContent", raw.CachedContent},
	} {
		if presentJSON(key.raw) {
			return fmt.Errorf("%s is not supported for OpenRouter channels; use %s", key.name, key.camel)
		}
	}
	// Checked on the raw body: GeminiHelper drops a systemInstruction without
	// text before conversion, so media-only instructions never reach the DTO.
	for _, instruction := range []json.RawMessage{raw.SystemInstruction, raw.SystemInstructionSnake} {
		if !presentJSON(instruction) {
			continue
		}
		var content struct {
			Parts []map[string]json.RawMessage `json:"parts"`
		}
		if common.Unmarshal(instruction, &content) != nil {
			return errors.New("systemInstruction must be a Gemini content object")
		}
		for _, part := range content.Parts {
			for key := range part {
				if key != "text" && key != "thought" {
					return errors.New("systemInstruction non-text parts are not supported for OpenRouter channels; only text parts are supported")
				}
			}
		}
	}
	if len(request.Requests) > 0 {
		return errors.New("batched Gemini requests are not supported for OpenRouter channels")
	}
	if request.CachedContent != "" {
		return errors.New("cachedContent is not supported for OpenRouter channels")
	}
	config := request.GenerationConfig
	if config.CandidateCount != nil && (*config.CandidateCount < 1 || *config.CandidateCount > dto.MaxImageN) {
		return fmt.Errorf("candidateCount must be between 1 and %d", dto.MaxImageN)
	}
	if config.ResponseMimeType != "" && !strings.EqualFold(config.ResponseMimeType, "text/plain") {
		return fmt.Errorf("generationConfig responseMimeType %q is not supported for OpenRouter channels; only text/plain is supported", config.ResponseMimeType)
	}
	// The converter keeps only the first four stop sequences.
	if len(config.StopSequences) > 4 {
		return errors.New("generationConfig stopSequences supports at most 4 entries for OpenRouter channels")
	}
	unsupported := []struct {
		field   string
		present bool
	}{
		{"generationConfig responseSchema", config.ResponseSchema != nil},
		{"generationConfig responseJsonSchema", presentJSON(config.ResponseJsonSchema)},
		{"generationConfig presencePenalty", config.PresencePenalty != nil},
		{"generationConfig frequencyPenalty", config.FrequencyPenalty != nil},
		{"generationConfig seed", config.Seed != nil},
		{"generationConfig responseLogprobs", config.ResponseLogprobs != nil},
		{"generationConfig logprobs", config.Logprobs != nil},
		{"generationConfig speechConfig", presentJSON(config.SpeechConfig)},
		{"generationConfig imageConfig", presentJSON(config.ImageConfig)},
		{"generationConfig mediaResolution", config.MediaResolution != ""},
		{"generationConfig enableEnhancedCivicAnswers", config.EnableEnhancedCivicAnswers != nil},
		{"toolConfig", request.ToolConfig != nil},
	}
	for _, field := range unsupported {
		if field.present {
			return fmt.Errorf("%s is not supported for OpenRouter channels", field.field)
		}
	}
	for _, modality := range config.ResponseModalities {
		if !strings.EqualFold(modality, "TEXT") {
			return fmt.Errorf("responseModalities %q is not supported for OpenRouter channels", modality)
		}
	}
	if presentJSON(request.Tools) {
		tools := bytes.TrimSpace(request.Tools)
		var entries []map[string]json.RawMessage
		if tools[0] == '{' {
			tools = append(append([]byte{'['}, tools...), ']')
		}
		if err := common.Unmarshal(tools, &entries); err != nil {
			return errors.New("tools must be an array of Gemini tool objects")
		}
		for _, entry := range entries {
			for name, value := range entry {
				if name != "functionDeclarations" {
					return fmt.Errorf("tool %q is not supported for OpenRouter channels; only functionDeclarations are supported", name)
				}
				if !presentJSON(value) {
					continue
				}
				// The converter keeps only name, description and parameters, and
				// silently drops every declaration when the list does not decode.
				var declarations []map[string]json.RawMessage
				if common.Unmarshal(value, &declarations) != nil {
					return errors.New("functionDeclarations must be an array of function declaration objects")
				}
				for _, declaration := range declarations {
					var functionName string
					if common.Unmarshal(declaration["name"], &functionName) != nil || functionName == "" {
						return errors.New("each function declaration requires a non-empty string name")
					}
					for key := range declaration {
						if key != "name" && key != "description" && key != "parameters" {
							return fmt.Errorf("function declaration field %q is not supported for OpenRouter channels; only name, description and parameters are supported", key)
						}
					}
				}
			}
		}
	}
	for _, content := range request.Contents {
		hasFunctionCall, hasTextOrMedia := false, false
		for _, part := range content.Parts {
			hasFunctionCall = hasFunctionCall || part.FunctionCall != nil
			hasTextOrMedia = hasTextOrMedia || part.Text != "" && !part.Thought || part.InlineData != nil || part.FileData != nil
			if part.FunctionResponse != nil && presentJSON(part.FunctionResponse.Parts) {
				return errors.New("functionResponse parts are not supported for OpenRouter channels")
			}
			if presentJSON(part.MediaResolution) {
				return errors.New("part mediaResolution is not supported for OpenRouter channels")
			}
			if part.ExecutableCode != nil || part.CodeExecutionResult != nil {
				return errors.New("executableCode and codeExecutionResult parts are not supported for OpenRouter channels")
			}
			if presentJSON(part.VideoMetadata) {
				return errors.New("part videoMetadata is not supported for OpenRouter channels")
			}
			// The converter keeps only the text of a part that also carries media.
			if part.Text != "" && (part.InlineData != nil || part.FileData != nil) {
				return errors.New("a part carrying both text and inlineData or fileData is not supported for OpenRouter channels; send them as separate parts")
			}
			mimeType := ""
			field := ""
			switch {
			case part.InlineData != nil:
				mimeType, field = part.InlineData.MimeType, "inlineData"
			case part.FileData != nil:
				mimeType, field = part.FileData.MimeType, "fileData"
			default:
				continue
			}
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "image/") {
				return fmt.Errorf("%s mimeType %q is not supported for OpenRouter channels; only image/* inputs are supported", field, mimeType)
			}
		}
		// A converted tool-call message carries only the calls; text or media
		// beside a functionCall in the same content would be lost.
		if hasFunctionCall && hasTextOrMedia {
			return errors.New("functionCall parts mixed with text or media in one content are not supported for OpenRouter channels")
		}
	}
	return nil
}

func presentJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && string(trimmed) != "null"
}
