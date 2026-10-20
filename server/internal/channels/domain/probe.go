package domain

import (
	"context"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type ProbeOutcome interface {
	LocalError() error
	APIError() *types.NewAPIError
	ChannelKey() string
	HandleFailure(*types.NewAPIError)
}
type Probe interface {
	Test(context.Context, *Channel, int, string, string, bool) ProbeOutcome
}
