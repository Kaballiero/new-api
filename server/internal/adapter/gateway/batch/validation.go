package batch

import (
	"encoding/json"

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
		return 0, &domain.RuleError{Kind: domain.ItemBodyInvalid}
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return 0, err
	}
	if raw, ok := fields["model"]; ok {
		var supplied string
		if common.Unmarshal(raw, &supplied) != nil || supplied != modelName {
			return 0, &domain.RuleError{Kind: domain.ItemModelMismatch}
		}
	}
	if raw, ok := fields["stream"]; ok {
		var stream bool
		if common.Unmarshal(raw, &stream) != nil || stream {
			return 0, &domain.RuleError{Kind: domain.ItemStreamingUnsupported}
		}
	}
	if raw, ok := fields["n"]; ok {
		var n uint
		if common.Unmarshal(raw, &n) != nil || n != 1 {
			return 0, &domain.RuleError{Kind: domain.ItemCountInvalid}
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
				return 0, &domain.RuleError{Kind: domain.OutputLimitUnsupported, Field: key, Endpoint: endpoint}
			}
			var value uint
			if common.Unmarshal(raw, &value) != nil || value == 0 || value > helper.MaxTokensLimit {
				return 0, &domain.RuleError{Kind: domain.OutputLimitOutOfRange, Field: key, Limit: helper.MaxTokensLimit}
			}
			maxOutput = max(maxOutput, int(value))
		}
	}
	if endpoint == "/v1beta/generateContent" {
		maxOutput = 0
		if _, exists := fields["generation_config"]; exists {
			return 0, &domain.RuleError{Kind: domain.UseGenerationConfig}
		}
		var config map[string]json.RawMessage
		if raw, ok := fields["generationConfig"]; ok {
			if common.HasDuplicateJSONKeys(raw) || common.Unmarshal(raw, &config) != nil {
				return 0, &domain.RuleError{Kind: domain.GenerationConfigInvalid}
			}
		}
		if _, ok := config["max_output_tokens"]; ok {
			return 0, &domain.RuleError{Kind: domain.UseMaxOutputTokens}
		}
		if _, ok := config["candidate_count"]; ok {
			return 0, &domain.RuleError{Kind: domain.UseCandidateCount}
		}
		if raw, ok := config["maxOutputTokens"]; ok {
			var value uint
			if common.Unmarshal(raw, &value) != nil || value == 0 || value > helper.MaxTokensLimit {
				return 0, &domain.RuleError{Kind: domain.MaxOutputTokensInvalid}
			}
			maxOutput = int(value)
		}
		if raw, ok := config["candidateCount"]; ok {
			var n uint
			if common.Unmarshal(raw, &n) != nil || n != 1 {
				return 0, &domain.RuleError{Kind: domain.CandidateCountInvalid}
			}
		}
	}
	if endpoint != "/v1/embeddings" && maxOutput == 0 {
		return 0, &domain.RuleError{Kind: domain.OutputLimitRequired}
	}
	return maxOutput, nil
}
