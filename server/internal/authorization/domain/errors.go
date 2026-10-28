package domain

import "errors"

// Business errors of authorization scenarios. No authorization scenario writes
// them to HTTP itself: they reach clients through the callers (account
// permission updates) and startup.
var (
	// ErrEnforcerNotInitialized reports a policy operation before the
	// enforcer was created by initialize_authorization.
	ErrEnforcerNotInitialized = errors.New("authz enforcer is not initialized")

	// ErrInternal classifies an unexpected failure (policy storage or the
	// enforcer). Create it with Internal so that the message stays the text of
	// the cause.
	ErrInternal = errors.New("internal error")
)

// Internal marks cause as an unexpected failure: errors.Is(err, ErrInternal)
// holds, errors.Unwrap returns cause and the message is the text of cause.
func Internal(cause error) error { return internalError{cause: cause} }

type internalError struct{ cause error }

func (e internalError) Error() string        { return e.cause.Error() }
func (e internalError) Unwrap() error        { return e.cause }
func (e internalError) Is(target error) bool { return target == ErrInternal }
