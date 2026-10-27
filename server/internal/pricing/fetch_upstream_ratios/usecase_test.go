package fetch_upstream_ratios

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"testing"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeChannels struct {
	rows []Channel
	err  error
}

func (c fakeChannels) ByIDs([]int) ([]Channel, error) { return c.rows, c.err }

type fakeClient struct{ body string }

func (c fakeClient) Fetch(context.Context, string, bool, int, string) ([]byte, func(), error) {
	return []byte(c.body), nil, nil
}

type fakeClients struct{ body string }

func (c fakeClients) NewClient() Client { return fakeClient(c) }

func newTestUsecase(channels Channels, body string, logged *[]string) Usecase {
	return NewUsecase(domain.Capabilities{Codec: domain.JSONCodec{DecodeJson: common.DecodeJson, Unmarshal: common.Unmarshal}}, channels, fakeClients{body: body}, func() map[string]any { return map[string]any{} },
		func(_ context.Context, message string) { *logged = append(*logged, message) },
		func(context.Context, string, ...any) {})
}

func TestExecuteRejectsMissingUpstreams(t *testing.T) {
	var logged []string
	errorText := func(err error) string { return err.Error() }

	_, err := newTestUsecase(fakeChannels{}, "", &logged).Execute(context.Background(), Input{Request: Request{Upstreams: []RatioUpstream{{Name: "ftp", BaseURL: "ftp://example"}}}, ErrorText: errorText})
	require.ErrorIs(t, err, domain.ErrNoUpstreams)

	_, err = newTestUsecase(fakeChannels{err: errors.New("db down")}, "", &logged).Execute(context.Background(), Input{Request: Request{ChannelIDs: []int64{1}}, ErrorText: errorText})
	require.ErrorIs(t, err, domain.ErrChannelQueryFailed)
	assert.Equal(t, []string{"failed to query channels: db down"}, logged)
}

func TestExecuteRendersUpstreamErrorsWithErrorText(t *testing.T) {
	var logged []string
	errorText := func(err error) string {
		if errors.Is(err, domain.ErrUpstreamDataUnrecognized) {
			return "localized"
		}
		return "raw: " + err.Error()
	}
	cases := []struct {
		body string
		want string
	}{
		{`{"success":true,"data":"text"}`, "localized"},
		{`{"success":false,"message":"upstream refused"}`, "raw: upstream refused"},
	}
	for _, tc := range cases {
		output, err := newTestUsecase(fakeChannels{rows: []Channel{{ID: 4, Name: "peer", BaseURL: "https://peer.example/"}}}, tc.body, &logged).
			Execute(context.Background(), Input{Request: Request{ChannelIDs: []int64{4}}, ErrorText: errorText})
		require.NoError(t, err)
		assert.Equal(t, []RatioTestResult{{Name: "peer(4)", Status: "error", Error: tc.want}}, output.TestResults)
	}
}

func TestExecuteKeepsEmptyUpstreamFailureAsSuccess(t *testing.T) {
	var logged []string
	output, err := newTestUsecase(fakeChannels{rows: []Channel{{ID: 4, Name: "peer", BaseURL: "https://peer.example/"}}}, `{"success":false}`, &logged).
		Execute(context.Background(), Input{Request: Request{ChannelIDs: []int64{4}}, ErrorText: func(err error) string { return err.Error() }})
	require.NoError(t, err)
	assert.Equal(t, []RatioTestResult{{Name: "peer(4)", Status: "success"}}, output.TestResults)
}
