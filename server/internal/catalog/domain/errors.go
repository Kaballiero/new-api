package domain

import "errors"

// Business errors of catalog scenarios. The transport maps each of them to the
// status, code and message of the HTTP response.
var (
	ErrModelNotFound   = errors.New("model not found")
	ErrModelIDMissing  = errors.New("model id is missing")
	ErrModelNameEmpty  = errors.New("model name is empty")
	ErrModelNameExists = errors.New("model name already exists")
	// ErrModelNameRequired rejects a blank name on update. Unlike
	// ErrModelNameEmpty on create it is answered without a status or code.
	ErrModelNameRequired = errors.New("model name is required")
	ErrInvalidVisibility = errors.New("invalid catalog visibility")

	ErrInvalidSquareState = errors.New("invalid model square state")
	ErrInvalidPagination  = errors.New("invalid pagination")

	// ErrPricingRootOnly rejects removing model pricing by a non-root admin.
	ErrPricingRootOnly = errors.New("model pricing is managed by a super administrator")

	ErrVendorNotFound   = errors.New("vendor not found")
	ErrVendorIDMissing  = errors.New("vendor id is missing")
	ErrVendorNameEmpty  = errors.New("vendor name is empty")
	ErrVendorNameExists = errors.New("vendor name already exists")

	// Vendor and metadata conflicts retain the existing message bytes
	// and reach clients unchanged.
	ErrVendorConflict       = errors.New("vendor data changed; preview again before applying")
	ErrMetadataSyncConflict = errors.New("metadata changed; preview again before applying")

	// Upstream metadata no longer matches the preview the selection was made on.
	ErrSyncSourceChanged    = errors.New("upstream metadata changed")
	ErrSyncModelUnavailable = errors.New("selected upstream model is no longer available")

	// ErrVendorRejected classifies a failed vendor change; create it with
	// RejectVendorChange.
	ErrVendorRejected = errors.New("vendor change rejected")
	// ErrSyncRejected classifies a failed metadata sync apply; create it with
	// RejectSync.
	ErrSyncRejected = errors.New("metadata sync rejected")

	// ErrInternal classifies an unexpected failure (usually storage). Create it
	// with Internal so that the message stays the text of the cause.
	ErrInternal = errors.New("internal error")
)

// Internal marks cause as an unexpected failure: errors.Is(err, ErrInternal)
// holds, errors.Unwrap returns cause and the message is the text of cause.
func Internal(cause error) error { return classifiedError{class: ErrInternal, cause: cause} }

// RejectVendorChange classifies a failed vendor change. A missing record
// becomes ErrVendorNotFound, any other failure ErrVendorRejected. The cause
// chain and text stay, so conflicts and reference errors remain detectable.
func RejectVendorChange(cause error, missing bool) error {
	if missing {
		return classifiedError{class: ErrVendorNotFound, cause: cause}
	}
	return classifiedError{class: ErrVendorRejected, cause: cause}
}

// RejectSync classifies a failed metadata sync apply as ErrSyncRejected and
// keeps the cause chain and text.
func RejectSync(cause error) error { return classifiedError{class: ErrSyncRejected, cause: cause} }

type classifiedError struct{ class, cause error }

func (e classifiedError) Error() string        { return e.cause.Error() }
func (e classifiedError) Unwrap() error        { return e.cause }
func (e classifiedError) Is(target error) bool { return target == e.class }
