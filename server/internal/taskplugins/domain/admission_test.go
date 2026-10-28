package domain

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginProtocolObservationLimiterCaps(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		limiter := NewObservationLimiter(ObservationLimits{
			Global:    2,
			PerPlugin: 2,
			PerUser:   2,
			PerToken:  2,
		})
		releaseFirst, err := limiter.Acquire("first", 1, 1)
		require.NoError(t, err)
		defer releaseFirst()
		releaseSecond, err := limiter.Acquire("second", 2, 2)
		require.NoError(t, err)
		defer releaseSecond()

		release, err := limiter.Acquire("third", 3, 3)
		assert.Nil(t, release)
		assertLimitError(t, err, "global", 2)
	})

	t.Run("plugin", func(t *testing.T) {
		limiter := NewObservationLimiter(ObservationLimits{
			Global:    3,
			PerPlugin: 1,
			PerUser:   3,
			PerToken:  3,
		})
		releaseFirst, err := limiter.Acquire("shared", 1, 1)
		require.NoError(t, err)
		defer releaseFirst()

		release, err := limiter.Acquire("shared", 2, 2)
		assert.Nil(t, release)
		assertLimitError(t, err, "plugin", 1)

		releaseOther, err := limiter.Acquire("other", 2, 2)
		require.NoError(t, err)
		defer releaseOther()
	})

	t.Run("user", func(t *testing.T) {
		limiter := NewObservationLimiter(ObservationLimits{
			Global:    3,
			PerPlugin: 3,
			PerUser:   1,
			PerToken:  3,
		})
		releaseFirst, err := limiter.Acquire("first", 1, 1)
		require.NoError(t, err)
		defer releaseFirst()

		release, err := limiter.Acquire("second", 1, 2)
		assert.Nil(t, release)
		assertLimitError(t, err, "user", 1)

		releaseOther, err := limiter.Acquire("second", 2, 2)
		require.NoError(t, err)
		defer releaseOther()
	})

	t.Run("token", func(t *testing.T) {
		limiter := NewObservationLimiter(ObservationLimits{
			Global:    3,
			PerPlugin: 3,
			PerUser:   3,
			PerToken:  1,
		})
		releaseFirst, err := limiter.Acquire("first", 1, 1)
		require.NoError(t, err)
		defer releaseFirst()

		release, err := limiter.Acquire("second", 2, 1)
		assert.Nil(t, release)
		assertLimitError(t, err, "token", 1)

		releaseOther, err := limiter.Acquire("second", 2, 2)
		require.NoError(t, err)
		defer releaseOther()
	})
}

func TestPluginProtocolObservationLimiterRollsBackFailedAdmission(t *testing.T) {
	t.Run("user failure", func(t *testing.T) {
		limiter := NewObservationLimiter(ObservationLimits{
			Global:    2,
			PerPlugin: 2,
			PerUser:   1,
			PerToken:  2,
		})
		releaseHeld, err := limiter.Acquire("first", 1, 1)
		require.NoError(t, err)
		defer releaseHeld()

		release, err := limiter.Acquire("second", 1, 2)
		assert.Nil(t, release)
		assertLimitError(t, err, "user", 1)

		releaseReplacement, err := limiter.Acquire("second", 2, 2)
		require.NoError(t, err)
		defer releaseReplacement()
	})

	t.Run("token failure", func(t *testing.T) {
		limiter := NewObservationLimiter(ObservationLimits{
			Global:    2,
			PerPlugin: 2,
			PerUser:   2,
			PerToken:  1,
		})
		releaseHeld, err := limiter.Acquire("first", 1, 1)
		require.NoError(t, err)
		defer releaseHeld()

		release, err := limiter.Acquire("second", 2, 1)
		assert.Nil(t, release)
		assertLimitError(t, err, "token", 1)

		releaseReplacement, err := limiter.Acquire("second", 2, 2)
		require.NoError(t, err)
		defer releaseReplacement()
	})
}

func TestPluginProtocolObservationLimiterReleaseIsIdempotent(t *testing.T) {
	limiter := NewObservationLimiter(ObservationLimits{
		Global:    1,
		PerPlugin: 1,
		PerUser:   1,
		PerToken:  1,
	})
	release, err := limiter.Acquire("plugin", 1, 1)
	require.NoError(t, err)

	release()
	release()
	assert.Zero(t, limiter.global)
	assert.Empty(t, limiter.plugin)
	assert.Empty(t, limiter.user)
	assert.Empty(t, limiter.token)

	releaseAgain, err := limiter.Acquire("plugin", 1, 1)
	require.NoError(t, err)
	releaseAgain()
}

func TestPluginProtocolObservationLimiterRejectsMissingIdentityWithoutConsumingCapacity(t *testing.T) {
	limiter := NewObservationLimiter(ObservationLimits{
		Global:    1,
		PerPlugin: 1,
		PerUser:   1,
		PerToken:  1,
	})

	for _, testCase := range []struct {
		name      string
		pluginKey string
		userID    int
		tokenID   int
	}{
		{name: "empty plugin", userID: 1, tokenID: 1},
		{name: "blank plugin", pluginKey: " \t", userID: 1, tokenID: 1},
		{name: "zero user", pluginKey: "plugin", tokenID: 1},
		{name: "negative user", pluginKey: "plugin", userID: -1, tokenID: 1},
		{name: "zero token", pluginKey: "plugin", userID: 1},
		{name: "negative token", pluginKey: "plugin", userID: 1, tokenID: -1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			release, err := limiter.Acquire(testCase.pluginKey, testCase.userID, testCase.tokenID)
			assert.Nil(t, release)
			assert.ErrorIs(t, err, ErrInvalidObservationIdentity)
		})
	}

	release, err := limiter.Acquire("plugin", 1, 1)
	require.NoError(t, err)
	release()
}

func TestPluginProtocolObservationLimiterConcurrentAdmissionsRespectCap(t *testing.T) {
	const (
		workerCount = 8
		globalLimit = 3
	)
	limiter := NewObservationLimiter(ObservationLimits{
		Global:    globalLimit,
		PerPlugin: workerCount,
		PerUser:   workerCount,
		PerToken:  workerCount,
	})
	start := make(chan struct{})
	releases := make(chan func(), workerCount)
	errorsFound := make(chan error, workerCount)

	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 1; worker <= workerCount; worker++ {
		go func(id int) {
			defer workers.Done()
			<-start
			release, err := limiter.Acquire("plugin", id, id)
			if err != nil {
				errorsFound <- err
				return
			}
			releases <- release
		}(worker)
	}
	close(start)
	workers.Wait()
	close(releases)
	close(errorsFound)

	assert.Len(t, releases, globalLimit)
	assert.Len(t, errorsFound, workerCount-globalLimit)
	for err := range errorsFound {
		assert.ErrorIs(t, err, ErrObservationLimitExceeded)
	}
	for release := range releases {
		release()
	}

	release, err := limiter.Acquire("plugin", 1, 1)
	require.NoError(t, err)
	release()
}

func assertLimitError(
	t *testing.T,
	err error,
	expectedScope string,
	expectedLimit int,
) {
	t.Helper()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrObservationLimitExceeded)

	var limitError *ObservationLimitError
	require.True(t, errors.As(err, &limitError))
	assert.Equal(t, expectedScope, limitError.Scope)
	assert.Equal(t, expectedLimit, limitError.Limit)
}
