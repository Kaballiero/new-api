package batch

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
)

type Usage struct{}

func (Usage) Decode(job *domain.Job, result domain.Result) (*dto.Usage, error) {
	if result.StatusCode < 200 || result.StatusCode >= 300 || len(result.Body) == 0 {
		return nil, errors.New("batch item requires billing reconciliation")
	}
	var envelope map[string]json.RawMessage
	if err := common.Unmarshal(result.Body, &envelope); err != nil {
		return nil, err
	}
	key := "usage"
	if job.Endpoint == "/v1beta/generateContent" {
		key = "usageMetadata"
	}
	raw := envelope[key]
	if common.GetJsonType(raw) != "object" || string(raw) == "{}" {
		return nil, errors.New("batch result has no usage facts")
	}
	var facts map[string]any
	if err := common.Unmarshal(raw, &facts); err != nil {
		return nil, err
	}
	if err := validateBatchUsage(facts); err != nil {
		return nil, err
	}
	required := []string{"prompt_tokens", "completion_tokens"}
	switch job.Endpoint {
	case "/v1/messages":
		required = []string{"input_tokens", "output_tokens"}
	case "/v1/responses":
		required = []string{"input_tokens", "output_tokens"}
	case "/v1/embeddings":
		required = []string{"prompt_tokens"}
	case "/v1beta/generateContent":
		required = []string{"promptTokenCount", "candidatesTokenCount"}
	}
	for _, key := range required {
		if _, ok := facts[key].(float64); !ok {
			return nil, fmt.Errorf("batch result has no %s usage fact", key)
		}
	}

	var usage *dto.Usage
	switch job.Endpoint {
	case "/v1/messages":
		var native dto.ClaudeUsage
		if err := common.Unmarshal(raw, &native); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromClaudeAPIUsage(&native)
	case "/v1beta/generateContent":
		var native dto.GeminiUsageMetadata
		if err := common.Unmarshal(raw, &native); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromGeminiMetadata(&native, 0)
	case "/v1/responses":
		var native dto.Usage
		if err := common.Unmarshal(raw, &native); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromResponsesUsage(&native)
	default:
		var native dto.Usage
		if err := common.Unmarshal(raw, &native); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromChatUsage(&native)
	}
	if usage == nil {
		return nil, errors.New("unrecognized batch usage")
	}
	return usage, nil
}

func validateBatchUsage(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, value := range typed {
			if number, ok := value.(float64); ok && strings.Contains(strings.ToLower(key), "token") && math.Trunc(number) != number {
				return errors.New("fractional batch token count")
			}
			if err := validateBatchUsage(value); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range typed {
			if err := validateBatchUsage(value); err != nil {
				return err
			}
		}
	case float64:
		if typed < 0 || typed > common.MaxQuota || math.IsNaN(typed) || math.IsInf(typed, 0) {
			return errors.New("invalid batch usage quantity")
		}
	}
	return nil
}
