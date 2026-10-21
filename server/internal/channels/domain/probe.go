package domain

import "context"

// ProbeOutcome is the result of a test request sent through the gateway.
type ProbeOutcome interface {
	// LocalError reports a failure before or outside the upstream exchange.
	LocalError() error
	// UpstreamError is the gateway failure of the test request, nil on success.
	UpstreamError() error
	// ErrorCode is the gateway error code of UpstreamError, empty without one.
	ErrorCode() string
	// ChannelKey is the channel key the gateway used for the request.
	ChannelKey() string
	// Disable hands failure back to the gateway's channel error handling.
	Disable(failure error)
}

// Probe sends a test request for a model of the channel through the gateway.
type Probe interface {
	Test(ctx context.Context, channel *Channel, userID int, model, endpoint string, stream bool) ProbeOutcome
}
