package domain

import "errors"

// Business errors of API key scenarios. The transport maps each of them to the
// status, code and localized message of the HTTP response.
var (
	ErrKeyNotFound           = errors.New("api key not found")
	ErrNameTooLong           = errors.New("api key name is too long")
	ErrNameDuplicate         = errors.New("api key name is already used by another key of the user")
	ErrQuotaNegative         = errors.New("api key quota is negative")
	ErrQuotaExceedMax        = errors.New("api key quota exceeds the maximum")
	ErrKeyCountLimit         = errors.New("api key count limit reached")
	ErrKeyGenerate           = errors.New("api key generation failed")
	ErrExpiredCannotEnable   = errors.New("expired api key cannot be enabled")
	ErrExhaustedCannotEnable = errors.New("exhausted api key cannot be enabled")
	ErrAutoGroupsTooMany     = errors.New("too many auto groups")
	ErrAutoGroupDuplicate    = errors.New("duplicate auto group")
	ErrAutoGroupInvalid      = errors.New("auto group is not selectable")
	ErrBatchEmpty            = errors.New("api key batch is empty")
	ErrBatchTooLarge         = errors.New("api key batch is too large")
	ErrUsageUnavailable      = errors.New("api key usage is unavailable")
	ErrKeyIDRequired         = errors.New("api key id or user id is empty")

	// Search errors are shared with usage and payment search through SanitizeSearchPattern.
	ErrSearchConsecutiveWildcards = errors.New("search pattern must not contain consecutive % wildcards")
	ErrSearchTooManyWildcards     = errors.New("search pattern allows at most 2 % wildcards")
	ErrSearchKeywordTooShort      = errors.New("fuzzy search keyword must be at least 2 characters")
	ErrSearchCountFailed          = errors.New("failed to count api keys")
	ErrFuzzySearchLimit           = errors.New("too many api keys for fuzzy search; use exact search without %")
	ErrSearchFailed               = errors.New("failed to search api keys")

	// Credential validation errors are mapped at the gateway transport boundary.
	ErrKeyNotProvided = errors.New("token not provided")
	ErrKeyInvalid     = errors.New("token invalid")

	// ErrInternal classifies an unexpected failure (usually storage). Create it
	// with Internal so that the message stays the text of the cause.
	ErrInternal = errors.New("internal error")
)

// Internal marks cause as an unexpected failure: errors.Is(err, ErrInternal)
// holds, errors.Unwrap returns cause and the message is the text of cause.
func Internal(cause error) error { return internalError{cause: cause} }

type internalError struct{ cause error }

func (e internalError) Error() string        { return e.cause.Error() }
func (e internalError) Unwrap() error        { return e.cause }
func (e internalError) Is(target error) bool { return target == ErrInternal }

// LimitError reports a violated numeric limit; errors.Is matches Err.
type LimitError struct {
	Err error
	Max int
}

func (e *LimitError) Error() string { return e.Err.Error() }
func (e *LimitError) Unwrap() error { return e.Err }

// GroupError reports a rejected auto group; errors.Is matches Err.
type GroupError struct {
	Err   error
	Group string
}

func (e *GroupError) Error() string { return e.Err.Error() }
func (e *GroupError) Unwrap() error { return e.Err }
