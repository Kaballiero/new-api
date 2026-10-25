package batch

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/relay/helper"
	providerrouting "github.com/QuantumNous/new-api/relay/provider"
)

type Validation struct{}

func (Validation) Provider(raw []byte) ([]byte, error) {
	routing, err := providerrouting.Extract(raw)
	if err != nil {
		return nil, err
	}
	return routing, providerrouting.Validate("batch", routing)
}
func (Validation) Item(body []byte, modelName, endpoint string) (int, error) {
	if len(body) == 0 || len(body) > domain.MaxResultBytes || common.GetJsonType(body) != "object" || common.HasDuplicateJSONKeys(body) {
		return 0, errors.New("body must be a bounded JSON object without duplicate keys")
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return 0, err
	}
	if raw, ok := fields["model"]; ok {
		var supplied string
		if common.Unmarshal(raw, &supplied) != nil || supplied != modelName {
			return 0, errors.New("item model must match the batch model")
		}
	}
	if raw, ok := fields["stream"]; ok {
		var stream bool
		if common.Unmarshal(raw, &stream) != nil || stream {
			return 0, errors.New("streaming is not supported in Batch")
		}
	}
	if raw, ok := fields["n"]; ok {
		var n uint
		if common.Unmarshal(raw, &n) != nil || n != 1 {
			return 0, errors.New("Batch requires n=1")
		}
	}
	maxOutput := 0
	allowedCaps := map[string]bool{}
	switch endpoint {
	case "/v1/chat/completions":
		allowedCaps["max_tokens"] = true
		allowedCaps["max_completion_tokens"] = true
	case "/v1/responses":
		allowedCaps["max_output_tokens"] = true
	case "/v1/messages":
		allowedCaps["max_tokens"] = true
	}
	for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		if raw, ok := fields[key]; ok {
			if !allowedCaps[key] {
				return 0, fmt.Errorf("%s is not a native output limit for %s", key, endpoint)
			}
			var value uint
			if common.Unmarshal(raw, &value) != nil || value == 0 || value > helper.MaxTokensLimit {
				return 0, fmt.Errorf("%s must be in 1..%d", key, helper.MaxTokensLimit)
			}
			maxOutput = max(maxOutput, int(value))
		}
	}
	if endpoint == "/v1beta/generateContent" {
		maxOutput = 0
		if _, exists := fields["generation_config"]; exists {
			return 0, errors.New("use generationConfig for Batch requests")
		}
		var config map[string]json.RawMessage
		if raw, ok := fields["generationConfig"]; ok {
			if common.HasDuplicateJSONKeys(raw) || common.Unmarshal(raw, &config) != nil {
				return 0, errors.New("invalid generationConfig")
			}
		}
		if _, ok := config["max_output_tokens"]; ok {
			return 0, errors.New("use maxOutputTokens for Batch requests")
		}
		if _, ok := config["candidate_count"]; ok {
			return 0, errors.New("use candidateCount for Batch requests")
		}
		if raw, ok := config["maxOutputTokens"]; ok {
			var value uint
			if common.Unmarshal(raw, &value) != nil || value == 0 || value > helper.MaxTokensLimit {
				return 0, errors.New("invalid maxOutputTokens")
			}
			maxOutput = int(value)
		}
		if raw, ok := config["candidateCount"]; ok {
			var n uint
			if common.Unmarshal(raw, &n) != nil || n != 1 {
				return 0, errors.New("Batch requires candidateCount=1")
			}
		}
	}
	if endpoint != "/v1/embeddings" && maxOutput == 0 {
		return 0, errors.New("an explicit output token limit is required for Batch")
	}
	return maxOutput, nil
}
