package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryHonorsPinRetryMode(t *testing.T) {
	openaiErr := types.NewOpenAIError(errors.New("upstream"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)

	c := newPinRetryContext()
	assert.True(t, shouldRetry(c, openaiErr, 1))

	origin := newPinRetryContext()
	service.GetChannelConstraints(origin).AddPin(dto.ChannelPin{
		ChannelId: 2,
		Source:    dto.PinSourceOriginTask,
		Rank:      dto.PinRankOriginTask,
		RetryMode: dto.PinRetrySameChannel,
	})
	assert.True(t, shouldRetry(origin, openaiErr, 1), "origin pin retries on the same channel")

	token := newPinRetryContext()
	service.GetChannelConstraints(token).AddPin(dto.ChannelPin{
		ChannelId: 1,
		Source:    dto.PinSourceToken,
		Rank:      dto.PinRankToken,
		RetryMode: dto.PinRetrySingleAttempt,
	})
	assert.False(t, shouldRetry(token, openaiErr, 1), "token pin suppresses retry")
}

func TestShouldRetryTaskRelayHonorsPinRetryMode(t *testing.T) {
	taskErr := &dto.TaskError{StatusCode: http.StatusInternalServerError}

	c := newPinRetryContext()
	assert.True(t, shouldRetryTaskRelay(c, 1, taskErr, 1))

	origin := newPinRetryContext()
	service.GetChannelConstraints(origin).AddPin(dto.ChannelPin{
		ChannelId: 2,
		Source:    dto.PinSourceOriginTask,
		Rank:      dto.PinRankOriginTask,
		RetryMode: dto.PinRetrySameChannel,
	})
	assert.True(t, shouldRetryTaskRelay(origin, 2, taskErr, 1))

	token := newPinRetryContext()
	service.GetChannelConstraints(token).AddPin(dto.ChannelPin{
		ChannelId: 1,
		Source:    dto.PinSourceToken,
		Rank:      dto.PinRankToken,
		RetryMode: dto.PinRetrySingleAttempt,
	})
	assert.False(t, shouldRetryTaskRelay(token, 1, taskErr, 1))
}

func TestSameChannelPinsMergeToStricterRetryMode(t *testing.T) {
	c := newPinRetryContext()
	constraints := service.GetChannelConstraints(c)
	constraints.AddPin(dto.ChannelPin{
		ChannelId: 7,
		Source:    dto.PinSourceOriginTask,
		Rank:      dto.PinRankOriginTask,
		RetryMode: dto.PinRetrySameChannel,
	})
	constraints.AddPin(dto.ChannelPin{
		ChannelId: 7,
		Source:    dto.PinSourceToken,
		Rank:      dto.PinRankToken,
		RetryMode: dto.PinRetrySingleAttempt,
	})
	pin, found, overridden := constraints.ResolvedPin()
	require.True(t, found)
	assert.Equal(t, 7, pin.ChannelId)
	assert.Equal(t, dto.PinRetrySingleAttempt, pin.RetryMode)
	assert.Empty(t, overridden)
	assert.False(t, shouldRetry(c, types.NewOpenAIError(errors.New("upstream"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError), 1))
}

func newPinRetryContext() *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func TestShouldRetryChannelErrorsRespectReplayGuards(t *testing.T) {
	for _, tc := range []struct {
		name                                        string
		budget                                      int
		skip, pin, written, cancelled, continuation bool
		want                                        bool
	}{
		{name: "safe pre-submit channel failure", budget: 1, want: true},
		{name: "exhausted budget", budget: 0},
		{name: "explicit skip", budget: 1, skip: true},
		{name: "single attempt pin", budget: 1, pin: true},
		{name: "stream already emitted", budget: 1, written: true},
		{name: "cancelled request", budget: 1, cancelled: true},
		{name: "provider bound continuation", budget: 1, continuation: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newPinRetryContext()
			if tc.written {
				_, err := c.Writer.Write([]byte("data: partial\n\n"))
				require.NoError(t, err)
			}
			if tc.cancelled {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			if tc.continuation {
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"previous_response_id":"resp_provider_bound"}`))
				c.Request.Header.Set("Content-Type", "application/json")
			}
			if tc.pin {
				service.GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 1, Source: dto.PinSourceToken, Rank: dto.PinRankToken, RetryMode: dto.PinRetrySingleAttempt})
			}
			opts := []types.NewAPIErrorOptions{}
			if tc.skip {
				opts = append(opts, types.ErrOptionWithSkipRetry())
			}
			err := types.NewError(errors.New("channel unavailable"), types.ErrorCodeChannelNoAvailableKey, opts...)
			assert.Equal(t, tc.want, shouldRetry(c, err, tc.budget))
		})
	}
}

func TestShouldRetryTaskRelayDoesNotReplayAfterOutputOrCancellation(t *testing.T) {
	for _, tc := range []string{"output", "cancel"} {
		t.Run(tc, func(t *testing.T) {
			c := newPinRetryContext()
			if tc == "output" {
				_, err := c.Writer.Write([]byte("partial"))
				require.NoError(t, err)
			} else {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			assert.False(t, shouldRetryTaskRelay(c, 1, &dto.TaskError{StatusCode: http.StatusTooManyRequests}, 1))
		})
	}
}
