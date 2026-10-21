package domain

import "errors"

// Business errors of channel scenarios. The transport maps each of them to the
// status, code and message of the legacy HTTP response. Several errors differ
// only by the response shape their scenario has always used.
var (
	// ErrChannelNotFound is answered with 404 channel_not_found.
	ErrChannelNotFound = errors.New("channel not found")
	// ErrChannelIDInvalid is answered with 200 and the English text.
	ErrChannelIDInvalid = errors.New("invalid channel id")
	// ErrStatusNotEditable rejects a status change through the general edit.
	ErrStatusNotEditable = errors.New("channel status is changed by status operations")
	// ErrSensitiveWriteDenied rejects a change of credentials, routing or
	// overrides without the channel sensitive write permission.
	ErrSensitiveWriteDenied = errors.New("channel sensitive write permission denied")
	// ErrTaskPluginBindDenied keeps the legacy English text for clients.
	ErrTaskPluginBindDenied = errors.New("task plugin channels require the task_plugin.bind permission")
	ErrAddModeUnsupported   = errors.New("unsupported add mode")

	// Channel validation. Texts that reached clients in English stay as they are.
	ErrChannelRequired             = errors.New("channel cannot be empty")
	ErrChannelSettingInvalid       = errors.New("invalid channel setting")
	ErrChannelKeyUnavailable       = errors.New("failed to get channel key")
	ErrTaskPluginKeyRequired       = errors.New("task plugin key is required")
	ErrTaskPluginKeyTooLong        = errors.New("task plugin key must not exceed 30 characters")
	ErrTaskPluginBaseURLRequired   = errors.New("base URL is required for task plugin channels")
	ErrNewAPIBaseURLRequired       = errors.New("New API channel base URL cannot be empty")
	ErrModelNameTooLong            = errors.New("model name is too long")
	ErrVertexRegionRequired        = errors.New("vertex deployment region is empty")
	ErrVertexRegionInvalid         = errors.New("vertex deployment region is not a JSON object")
	ErrVertexRegionDefaultMissing  = errors.New("vertex deployment region has no default entry")
	ErrCodexKeyInvalid             = errors.New("Codex key must be a valid JSON object")
	ErrCodexKeyAccessTokenRequired = errors.New("Codex key JSON must include access_token")
	ErrCodexKeyAccountIDRequired   = errors.New("Codex key JSON must include account_id")
	ErrVertexKeysInvalid           = errors.New("vertex keys must be a JSON array")
	ErrVertexKeyEncode             = errors.New("vertex key JSON encoding failed")
	ErrVertexKeysEmpty             = errors.New("vertex keys are empty")
	ErrAppendKeysInvalid           = errors.New("keys to append are invalid")

	// Tag operations.
	ErrTagRequired           = errors.New("tag is required")
	ErrParamOverrideInvalid  = errors.New("param override must be valid JSON")
	ErrHeaderOverrideInvalid = errors.New("header override must be valid JSON")

	// Copy.
	ErrCopySourceUnavailable = errors.New("failed to read the channel to copy")
	ErrCopySettingsInvalid   = errors.New("Failed to copy channel: invalid channel settings")
	ErrCopyFailed            = errors.New("failed to copy channel")

	// Listing.
	ErrListTagsFailed        = errors.New("failed to list channel tags")
	ErrCountTagsFailed       = errors.New("failed to count channel tags")
	ErrListTagChannelsFailed = errors.New("failed to list tag channels")
	ErrCountChannelsFailed   = errors.New("failed to count channels")
	ErrListChannelsFailed    = errors.New("failed to list channels")
	ErrCountTypesFailed      = errors.New("failed to count channel types")

	// Multi-key management, answered with 200 {success:false}.
	ErrMultiKeyChannelNotFound   = errors.New("multi-key channel not found")
	ErrNotMultiKey               = errors.New("channel is not in multi-key mode")
	ErrDisableKeyIndexRequired   = errors.New("key index to disable is not specified")
	ErrEnableKeyIndexRequired    = errors.New("key index to enable is not specified")
	ErrDeleteKeyIndexRequired    = errors.New("key index to delete is not specified")
	ErrKeyIndexOutOfRange        = errors.New("key index is out of range")
	ErrNoKeysToDisable           = errors.New("no enabled keys to disable")
	ErrLastKeyNotDeletable       = errors.New("the last key cannot be deleted")
	ErrNoAutoDisabledKeys        = errors.New("no automatically disabled keys to delete")
	ErrMultiKeyActionUnsupported = errors.New("unsupported multi-key action")

	// Balance.
	ErrBalanceTaskPlugin     = errors.New("Task Plugin channels do not support balance queries")
	ErrBalanceMultiKey       = errors.New("multi-key channels do not support balance queries")
	ErrBalanceNotImplemented = errors.New("balance query is not implemented for this channel type")

	// Upstream model discovery: ErrModelFetchFailed classifies a failed
	// discovery; create it with ModelFetchFailed.
	ErrModelFetchFailed = errors.New("failed to fetch models")
	// ErrNoModelsToValidate keeps its English text for clients.
	ErrNoModelsToValidate = errors.New("no models to validate")
	// ErrFetchConfigChanged reports a channel edited during discovery.
	ErrFetchConfigChanged = errors.New("channel fetch configuration changed during upstream model discovery")

	// Ollama. The texts reach clients as they are.
	ErrOllamaInputRequired = errors.New("Channel ID and model name are required")
	ErrOllamaNotFound      = errors.New("Channel not found")
	ErrNotOllamaChannel    = errors.New("This operation is only supported for Ollama channels")
	ErrOllamaPullFailed    = errors.New("Failed to pull model")
	ErrOllamaDeleteFailed  = errors.New("Failed to delete model")
	// ErrOllamaVersionFailed and ErrOllamaStreamFailed classify provider
	// failures; create them with OllamaVersionFailed and OllamaStreamFailed.
	ErrOllamaVersionFailed = errors.New("failed to get Ollama version")
	ErrOllamaStreamFailed  = errors.New("ollama model pull stream failed")

	// Codex. English texts reach clients as they are.
	ErrCodexChannelNotFound      = errors.New("channel not found")
	ErrNotCodexChannel           = errors.New("channel type is not Codex")
	ErrCodexMultiKey             = errors.New("multi-key channel is not supported")
	ErrCodexCredentialInvalid    = errors.New("codex credential is invalid")
	ErrCodexAccessTokenRequired  = errors.New("codex channel: access_token is required")
	ErrCodexAccountIDRequired    = errors.New("codex channel: account_id is required")
	ErrCodexRefreshTokenRequired = errors.New("codex channel: refresh_token is required to refresh credential")
	ErrCodexUsageFailed          = errors.New("failed to fetch codex usage")
	ErrCodexResetCreditsFailed   = errors.New("failed to fetch codex reset credits")
	ErrCodexResetFailed          = errors.New("failed to reset codex usage")

	// ErrRejected classifies a request rejected with 400 invalid_params and the
	// text of the cause; create it with Reject.
	ErrRejected = errors.New("channel request rejected")
	// ErrValidation classifies a channel rejected with 400
	// channel_validation_failed; create it with Invalid.
	ErrValidation = errors.New("channel validation failed")
	// ErrInternal classifies an unexpected failure (usually storage). Create it
	// with Internal so that the message stays the text of the cause.
	ErrInternal = errors.New("internal error")
)

// Internal marks cause as an unexpected failure: errors.Is(err, ErrInternal)
// holds, errors.Unwrap returns cause and the message is the text of cause.
func Internal(cause error) error { return classifiedError{class: ErrInternal, cause: cause} }

// Invalid marks cause as a failed channel validation.
func Invalid(cause error) error { return classifiedError{class: ErrValidation, cause: cause} }

// Reject marks cause as a request rejected with invalid_params.
func Reject(cause error) error { return classifiedError{class: ErrRejected, cause: cause} }

// ModelFetchFailed marks cause as a failed upstream model discovery.
func ModelFetchFailed(cause error) error {
	return classifiedError{class: ErrModelFetchFailed, cause: cause}
}

// OllamaVersionFailed marks cause as a failed Ollama version request.
func OllamaVersionFailed(cause error) error {
	return classifiedError{class: ErrOllamaVersionFailed, cause: cause}
}

// OllamaStreamFailed marks cause as a failure after the pull stream started.
func OllamaStreamFailed(cause error) error {
	return classifiedError{class: ErrOllamaStreamFailed, cause: cause}
}

// classifiedError keeps the text and the chain of cause and matches class.
type classifiedError struct{ class, cause error }

func (e classifiedError) Error() string        { return e.cause.Error() }
func (e classifiedError) Unwrap() error        { return e.cause }
func (e classifiedError) Is(target error) bool { return target == e.class }

// DetailError adds a detail to a business error; errors.Is matches Err and,
// when set, Cause. The detail is either Detail or the message of Cause.
type DetailError struct {
	Err    error
	Detail string
	Cause  error
}

func (e *DetailError) Error() string {
	if e.Cause != nil {
		return e.Err.Error() + ": " + e.Cause.Error()
	}
	return e.Err.Error() + ": " + e.Detail
}

func (e *DetailError) Unwrap() []error {
	if e.Cause != nil {
		return []error{e.Err, e.Cause}
	}
	return []error{e.Err}
}
