package channels

import (
	"context"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type ProbeEngine struct {
	run func(context.Context, *domain.Channel, int, string, string, bool) domain.ProbeOutcome
}

func NewProbeEngine(run func(context.Context, *domain.Channel, int, string, string, bool) domain.ProbeOutcome) ProbeEngine {
	return ProbeEngine{run: run}
}
func (engine ProbeEngine) Test(ctx context.Context, channel *domain.Channel, userID int, model, endpoint string, stream bool) domain.ProbeOutcome {
	return engine.run(ctx, channel, userID, model, endpoint, stream)
}

type ProbeOutcome struct {
	local   error
	api     *types.NewAPIError
	key     string
	failure func(*types.NewAPIError)
}

func NewProbeOutcome(local error, api *types.NewAPIError, key string, failure func(*types.NewAPIError)) ProbeOutcome {
	return ProbeOutcome{local: local, api: api, key: key, failure: failure}
}
func (result ProbeOutcome) LocalError() error                    { return result.local }
func (result ProbeOutcome) APIError() *types.NewAPIError         { return result.api }
func (result ProbeOutcome) ChannelKey() string                   { return result.key }
func (result ProbeOutcome) HandleFailure(err *types.NewAPIError) { result.failure(err) }
