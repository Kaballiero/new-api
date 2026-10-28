package run_channel_tests

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/channels/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOutcome struct {
	local, upstream error
	disabled        []error
}

func (o *fakeOutcome) LocalError() error     { return o.local }
func (o *fakeOutcome) UpstreamError() error  { return o.upstream }
func (o *fakeOutcome) ErrorCode() string     { return "" }
func (o *fakeOutcome) ChannelKey() string    { return "key" }
func (o *fakeOutcome) Disable(failure error) { o.disabled = append(o.disabled, failure) }

type fakeProbe struct {
	outcome *fakeOutcome
}

func (p fakeProbe) Test(context.Context, *domain.Channel, int, string, string, bool) domain.ProbeOutcome {
	return p.outcome
}

type fakeStore struct{ saved int64 }

func (s *fakeStore) All() ([]*domain.Channel, error) { return nil, nil }
func (s *fakeStore) SaveResponseTime(_ *domain.Channel, ms int64) {
	s.saved = ms
}

type fakeFailures struct {
	disable bool
	slow    error
}

func (f fakeFailures) ShouldDisable(error) bool { return f.disable }
func (f fakeFailures) ShouldEnable(failure error, status int) bool {
	return failure == nil && status == 3
}
func (f fakeFailures) ResponseTimeExceeded(int, float64, float64) error { return f.slow }

func newHealthCheck(probe fakeProbe, failures fakeFailures, enabled *[]int) Usecase {
	return NewUsecase(&fakeStore{}, probe, nil, func(*domain.Channel) bool { return false }, failures, func() bool { return true }, func(id int, _, _ string) { *enabled = append(*enabled, id) }, nil, nil, nil, nil, nil, nil)
}

// A negative threshold makes every probe slower than the threshold.
func TestHealthCheckDisablesSlowChannel(t *testing.T) {
	autoBan := 1
	channel := &domain.Channel{Id: 1, Status: 1, AutoBan: &autoBan}
	slow := errors.New("too slow")
	outcome := &fakeOutcome{}
	var enabled []int
	summary := newHealthCheck(fakeProbe{outcome: outcome}, fakeFailures{slow: slow}, &enabled).HealthCheck(context.Background(), channel, 1, true, -1)
	assert.Equal(t, domain.HealthSummary{Tested: 1, Failed: 1, Disabled: 1}, summary)
	require.Len(t, outcome.disabled, 1)
	assert.Same(t, slow, outcome.disabled[0])
	assert.Empty(t, enabled)
}

func TestHealthCheckEnablesRecoveredChannel(t *testing.T) {
	channel := &domain.Channel{Id: 2, Status: 3}
	outcome := &fakeOutcome{}
	var enabled []int
	summary := newHealthCheck(fakeProbe{outcome: outcome}, fakeFailures{}, &enabled).HealthCheck(context.Background(), channel, 1, true, 10_000)
	assert.Equal(t, domain.HealthSummary{Tested: 1, Succeeded: 1, Enabled: 1}, summary)
	assert.Equal(t, []int{2}, enabled)
	assert.Empty(t, outcome.disabled)
}

func TestHealthCheckKeepsChannelWithoutAutoBan(t *testing.T) {
	channel := &domain.Channel{Id: 3, Status: 1}
	outcome := &fakeOutcome{upstream: errors.New("bad response status code 401")}
	var enabled []int
	summary := newHealthCheck(fakeProbe{outcome: outcome}, fakeFailures{disable: true}, &enabled).HealthCheck(context.Background(), channel, 1, true, 10_000)
	assert.Equal(t, domain.HealthSummary{Tested: 1, Failed: 1}, summary)
	assert.Empty(t, outcome.disabled)
}
