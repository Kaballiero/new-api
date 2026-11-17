package domain

import (
	"errors"
	"fmt"
)

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

	// ErrNoCompatibleEndpoint reports that no endpoint of the routable models
	// passed the filters of an OpenRouter request; the reasons, if any, follow
	// the message.
	ErrNoCompatibleEndpoint = errors.New("OpenRouter request has no compatible priced endpoint")
	// ErrEndpointPriceMissing reports an OpenRouter endpoint price that is
	// absent or null: unknown, not zero. Its text is that of an invalid price.
	ErrEndpointPriceMissing = errors.New("invalid OpenRouter endpoint price")
	// ErrHostedSearchUnpriced reports web searches of an OpenRouter request
	// that neither an endpoint price nor the channel reserve covers.
	ErrHostedSearchUnpriced = errors.New("OpenRouter hosted-search quantity is unpriced without a positive approximate channel reserve")
)

// CatalogModelLossError rejects an upstream catalog revision that lost more
// than MaxCatalogModelLossPercent of the published models: the head stays at
// HeadRemoteRevision.
type CatalogModelLossError struct {
	RemoteRevision     string
	HeadRemoteRevision string
	Lost               []string
	Published          int
}

func (e CatalogModelLossError) Error() string {
	return fmt.Sprintf("upstream catalog revision lost too many published models: remote_revision=%s lost=%d published=%d head=%s", e.RemoteRevision, len(e.Lost), e.Published, e.HeadRemoteRevision)
}

// EstimateUnavailableError is a monetary metadata failure, not a route or
// transport failure. Only an explicit local reserve policy may cover it.
type EstimateUnavailableError struct{ Cause error }

func (e *EstimateUnavailableError) Error() string {
	return "OpenRouter estimate unavailable: " + e.Cause.Error()
}
func (e *EstimateUnavailableError) Unwrap() error { return e.Cause }

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
