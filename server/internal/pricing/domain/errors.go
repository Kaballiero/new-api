package domain

import "errors"

// Business errors of pricing scenarios. The transport maps each of them to the
// status, code and message of the HTTP response; the texts reach clients as is
// unless the transport replaces them with an i18n message.
var (
	// ErrInvalidQuery classifies a rejected pricing query; create it with
	// InvalidQuery so that the message stays the text of the cause.
	ErrInvalidQuery = errors.New("invalid pricing query")
	// ErrInvalidUserGroup rejects a group that is not configured or not
	// selectable for pricing.
	ErrInvalidUserGroup = errors.New("invalid user pricing group")
	// ErrPricingUsingGroup rejects a using_group the pricing group cannot use.
	ErrPricingUsingGroup = errors.New("invalid using_group")
	// ErrUserGroupUnavailable reports that the caller's group could not be read.
	ErrUserGroupUnavailable = errors.New("failed to resolve user pricing group")
	// ErrRevisionMismatch rejects a read whose expected_revision is stale.
	ErrRevisionMismatch = errors.New("Pricing catalog revision changed")
	// ErrModelPricingUnavailable reports a model without a published tariff.
	ErrModelPricingUnavailable = errors.New("model pricing is unavailable")
	// ErrPublicPricingUnavailable reports that the default tariff could not be
	// projected.
	ErrPublicPricingUnavailable = errors.New("failed to resolve public pricing")

	// Upstream ratio sync errors.
	ErrNoUpstreams              = errors.New("no valid upstream channel")
	ErrChannelQueryFailed       = errors.New("failed to query channels")
	ErrUpstreamDataUnrecognized = errors.New("upstream data format is not recognized")

	// ErrModelPricingConflict is returned directly to consumers.
	// Its message reaches clients unchanged.
	ErrModelPricingConflict = errors.New("model pricing changed; reload before saving")
	// ErrCatalogSourceChanged rejects a stale source snapshot
	// before publication.
	ErrCatalogSourceChanged = errors.New("catalog source changed before publication")
)

// InvalidQuery marks cause as a rejected pricing query: errors.Is(err,
// ErrInvalidQuery) holds, errors.Unwrap returns cause and the message is the
// text of cause.
func InvalidQuery(cause error) error { return invalidQueryError{cause: cause} }

type invalidQueryError struct{ cause error }

func (e invalidQueryError) Error() string        { return e.cause.Error() }
func (e invalidQueryError) Unwrap() error        { return e.cause }
func (e invalidQueryError) Is(target error) bool { return target == ErrInvalidQuery }

// CheckRevision rejects a read whose expected revision differs from the
// captured catalog revision; an empty expectation always matches.
func CheckRevision(expected, actual string) error {
	if expected != "" && expected != actual {
		return ErrRevisionMismatch
	}
	return nil
}
