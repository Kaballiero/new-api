package channels

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
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

// ProbeOutcome adapts the gateway test result to domain.ProbeOutcome.
type ProbeOutcome struct {
	local   error
	api     *types.NewAPIError
	key     string
	failure func(*types.NewAPIError)
}

func NewProbeOutcome(local error, api *types.NewAPIError, key string, failure func(*types.NewAPIError)) ProbeOutcome {
	return ProbeOutcome{local: local, api: api, key: key, failure: failure}
}
func (result ProbeOutcome) LocalError() error { return result.local }
func (result ProbeOutcome) UpstreamError() error {
	// A nil *NewAPIError must stay a nil error.
	if result.api == nil {
		return nil
	}
	return result.api
}
func (result ProbeOutcome) ErrorCode() string  { return string(result.api.GetErrorCode()) }
func (result ProbeOutcome) ChannelKey() string { return result.key }
func (result ProbeOutcome) Disable(failure error) {
	result.failure(GatewayError(failure))
}

// TestFailures applies the gateway's channel failure policy to channel tests.
type TestFailures struct{}

func (TestFailures) ShouldDisable(failure error) bool {
	return service.ShouldDisableChannel(GatewayError(failure))
}
func (TestFailures) ShouldEnable(failure error, status int) bool {
	return service.ShouldEnableChannel(GatewayError(failure), status)
}

// ResponseTimeExceeded phrases the failure in the language of the testing
// user; the text becomes the disable reason of the channel.
func (TestFailures) ResponseTimeExceeded(userID int, elapsed, threshold float64) error {
	message := i18n.Translate(model.GetUserLanguage(userID), i18n.MsgChannelResponseTimeExceeded, map[string]any{"Elapsed": fmt.Sprintf("%.2f", elapsed), "Threshold": fmt.Sprintf("%.2f", threshold)})
	return types.NewOpenAIError(errors.New(message), types.ErrorCodeChannelResponseTimeExceeded, http.StatusRequestTimeout)
}

// GatewayError returns the gateway error carried by failure, nil without one.
func GatewayError(failure error) *types.NewAPIError {
	var apiErr *types.NewAPIError
	if errors.As(failure, &apiErr) {
		return apiErr
	}
	return nil
}
