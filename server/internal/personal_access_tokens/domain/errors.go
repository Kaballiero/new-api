package domain

import "errors"

// Business errors of personal access token scenarios. The transport maps each
// of them to the status, code and localized message of the HTTP response.
var (
	// ErrTokenDuplicate reports that a freshly generated token already belongs
	// to another account; the client may simply retry.
	ErrTokenDuplicate = errors.New("generated access token already exists")
	// ErrUserIDRequired is returned by storage when a token is written without
	// an account id.
	ErrUserIDRequired = errors.New("access token user id is empty")

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
