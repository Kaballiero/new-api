package batch

import (
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
)

var batchProviderRequestID = regexp.MustCompile(`^(req[-_][0-9a-fA-F-]{1,120}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

// Match the entire known rejection, never arbitrary substrings of provider text.
// The model identifier is used only to recognize the template; it is not stored.
var batchMissingEndpoint = regexp.MustCompile(`^Model '[a-zA-Z0-9_.:/-]{1,191}' does not have a :batch endpoint\.$`)

// normalizedBatchFailure keeps an allowlisted classification rather than trying
// to redact arbitrary provider text, which can contain credentials or prompts.
func (Gateway) Failure(phase string, status int, requestID string, body []byte) string {
	diagnostic := domain.Failure{Version: 1, Phase: phase, Code: "provider_error", Category: "unknown",
		Message: "The provider could not process the Batch operation.", HTTPStatus: status}
	if batchProviderRequestID.MatchString(requestID) {
		diagnostic.RequestID = requestID
	}
	if len(body) <= 16<<10 {
		type providerFailure struct {
			Code     json.RawMessage `json:"code"`
			Type     string          `json:"type"`
			Status   string          `json:"status"`
			Message  string          `json:"message"`
			Metadata struct {
				ErrorType string `json:"error_type"`
			} `json:"metadata"`
			Error json.RawMessage `json:"error"`
		}
		var native providerFailure
		err := common.Unmarshal(body, &native)
		if err == nil && len(native.Error) > 0 && string(native.Error) != "null" {
			nested := native.Error
			native = providerFailure{}
			err = common.Unmarshal(nested, &native)
		}
		if err == nil {
			var code string
			if common.Unmarshal(native.Code, &code) != nil {
				var numericCode int
				if common.Unmarshal(native.Code, &numericCode) == nil && numericCode >= 100 && numericCode <= 599 {
					diagnostic.Code = strconv.Itoa(numericCode)
				}
			}
			// Prefer the typed OpenRouter reason over a generic protocol code.
			// Anthropic uses type; Google uses status. No raw metadata is retained.
			for _, candidate := range []string{native.Metadata.ErrorType, code, native.Type, native.Status} {
				switch candidate {
				case "invalid_request", "invalid_request_error", "invalid_argument", "INVALID_ARGUMENT", "context_length_exceeded":
					diagnostic.Category = "invalid_request"
					diagnostic.Message = "The provider rejected the Batch request parameters."
				case "model_not_found", "no_endpoints_found":
					diagnostic.Category = "endpoint_unavailable"
					diagnostic.Message = "The provider has no eligible endpoint for this Batch request."
				case "rate_limit_exceeded", "rate_limit_error", "RESOURCE_EXHAUSTED":
					diagnostic.Category = "rate_limit"
					diagnostic.Message = "The provider rejected the Batch request because of a resource or rate limit."
				case "provider_overloaded", "overloaded_error":
					diagnostic.Category = "capacity"
					diagnostic.Message = "The provider reported insufficient capacity for the Batch request."
				case "authentication", "permission_denied", "authentication_error", "permission_error", "PERMISSION_DENIED", "UNAUTHENTICATED":
					diagnostic.Category = "access"
					diagnostic.Message = "The provider rejected access to the Batch operation."
				case "batch_expired":
					diagnostic.Category = "expired"
					diagnostic.Message = "The provider Batch expired."
				default:
					continue
				}
				diagnostic.Code = candidate
				break
			}
			if diagnostic.Category == "unknown" && batchMissingEndpoint.MatchString(native.Message) {
				diagnostic.Code = "batch_endpoint_unavailable"
				diagnostic.Category = "endpoint_unavailable"
				diagnostic.Message = "The provider reported that the requested model has no Batch endpoint."
			}
		}
	}
	encoded, _ := common.Marshal(diagnostic)
	return string(encoded)
}
