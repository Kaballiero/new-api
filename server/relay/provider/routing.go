// Package provider owns aggregator provider preferences, independently of channel
// selection, credentials and billing. Provider identifiers are local to a namespace.
package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

const NamespaceOpenRouter = "openrouter"

type capability struct {
	channelType int
	namespace   string
	operations  []string
}

// New aggregators must declare their own namespace and operations here; an
// identical provider slug is not evidence of compatible routing semantics.
var capabilities = []capability{{constant.ChannelTypeOpenRouter, NamespaceOpenRouter, []string{
	"chat/completions", "messages", "responses", "embeddings", "rerank", "images/generations", "images/edits", "batch", "audio/speech", "audio/transcriptions", "decisions",
}}}

func CompatibleChannelTypes(namespace, operation string) []int {
	var types []int
	for _, candidate := range capabilities {
		if candidate.namespace == namespace && slices.Contains(candidate.operations, operation) {
			types = append(types, candidate.channelType)
		}
	}
	return types
}

func Operation(path string) string {
	if path == "/api/alpha/decisions" {
		return "decisions"
	}
	path = strings.TrimPrefix(strings.TrimPrefix(path, "/v1/"), "/pg/")
	if path == "batches" {
		return "batch"
	}
	return path
}

// Extract accepts both actual SDK wire JSON (top-level provider) and the legacy
// raw extra_body.provider shape. Equal values coalesce; conflicts never overwrite.
func Extract(body []byte) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	direct := fields["provider"]
	var extra map[string]json.RawMessage
	if raw := fields["extra_body"]; len(raw) > 0 {
		// extra_body also belongs to other providers; do not reinterpret its
		// non-object dialects when there is no nested provider to extract.
		_ = common.Unmarshal(raw, &extra)
	}
	nested := extra["provider"]
	if len(direct) > 0 || len(nested) > 0 {
		if common.HasDuplicateJSONKeys(body) || common.HasDuplicateJSONKeys(fields["extra_body"]) || common.HasDuplicateJSONKeys(direct) || common.HasDuplicateJSONKeys(nested) {
			return nil, errors.New("provider preferences must not contain duplicate JSON keys")
		}
	}
	if len(direct) > 0 && len(nested) > 0 {
		var a, b any
		if common.Unmarshal(direct, &a) != nil || common.Unmarshal(nested, &b) != nil || !reflect.DeepEqual(a, b) {
			return nil, errors.New("conflicting provider and extra_body.provider")
		}
	}
	if len(direct) > 0 {
		return direct, nil
	}
	return nested, nil
}

// ExtractForm reads the JSON-valued preferences in multipart fields without
// interpreting unrelated provider-specific form parameters.
func ExtractForm(values map[string][]string) (json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	for _, key := range []string{"provider", "extra_body"} {
		if values, exists := values[key]; exists {
			if len(values) != 1 {
				return nil, fmt.Errorf("%s must be provided once", key)
			}
			fields[key] = json.RawMessage(values[0])
		}
	}
	body, err := common.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return Extract(body)
}

func Normalize(body []byte) ([]byte, error) {
	raw, err := Extract(body)
	if err != nil || len(raw) == 0 {
		return body, err
	}
	var fields map[string]json.RawMessage
	if err = common.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	fields["provider"] = raw
	var extra map[string]json.RawMessage
	if common.Unmarshal(fields["extra_body"], &extra) == nil {
		delete(extra, "provider")
		if len(extra) == 0 {
			delete(fields, "extra_body")
		} else if fields["extra_body"], err = common.Marshal(extra); err != nil {
			return nil, err
		}
	}
	return common.Marshal(fields)
}

// Validate checks only the operation's documented preference vocabulary. Endpoint
// compatibility and prices remain the adapter's responsibility. Keep nullable
// values and empty arrays intact rather than inventing upstream default semantics.
func Validate(operation string, raw json.RawMessage) error {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var fields map[string]json.RawMessage
	if common.HasDuplicateJSONKeys(raw) || common.Unmarshal(raw, &fields) != nil || fields == nil {
		return errors.New("provider must be an object or null")
	}
	allowed := []string{"only", "order", "ignore", "allow_fallbacks", "require_parameters", "data_collection", "zdr", "enforce_distillable_text", "quantizations", "sort", "preferred_min_throughput", "preferred_max_latency", "max_price", "options"}
	switch operation {
	case "chat/completions", "messages", "responses", "embeddings", "rerank", "decisions":
	case "images/generations", "images/edits":
		allowed = []string{"only", "order", "ignore", "allow_fallbacks", "sort", "options"}
	case "batch":
		allowed = []string{"only", "allow_fallbacks"}
	case "audio/speech", "audio/transcriptions":
		allowed = []string{"zdr", "data_collection", "options"}
	default:
		return errors.New("provider preferences are unsupported for this operation")
	}
	for key, value := range fields {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("unsupported provider field %q for %s", key, operation)
		}
		if strings.TrimSpace(string(value)) == "null" {
			continue
		}
		switch key {
		case "only", "order", "ignore", "quantizations":
			var values []string
			if common.Unmarshal(value, &values) != nil || values == nil {
				return fmt.Errorf("provider.%s must be an array of strings", key)
			}
			for _, id := range values {
				if strings.TrimSpace(id) == "" {
					return fmt.Errorf("provider.%s contains an empty identifier", key)
				}
			}
		case "allow_fallbacks", "require_parameters", "zdr", "enforce_distillable_text":
			var flag bool
			if common.Unmarshal(value, &flag) != nil {
				return fmt.Errorf("provider.%s must be a boolean", key)
			}
		case "options", "max_price":
			var object map[string]json.RawMessage
			if common.Unmarshal(value, &object) != nil || object == nil {
				return fmt.Errorf("provider.%s must be an object", key)
			}
		}
	}
	return nil
}
