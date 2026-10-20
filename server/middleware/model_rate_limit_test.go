package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRedisRateLimitUsesUTCRegardlessOfLocalTimezone(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	previousLocation := time.Local
	time.Local = time.FixedZone("test-utc-plus-eight", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocation })

	ctx := context.Background()
	recordKey := "rateLimit:model-utc-record"
	recordRedisRequest(ctx, redisClient, recordKey, 2)
	recorded, err := redisClient.LIndex(ctx, recordKey, 0).Result()
	require.NoError(t, err)
	recordedAt, err := time.Parse(modelRateLimitTimeFormat, recorded)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC(), recordedAt, 2*time.Second)

	checkKey := "rateLimit:model-utc-check"
	withinWindow := time.Now().UTC().Add(-30 * time.Second).Format(modelRateLimitTimeFormat)
	_, err = redisServer.Push(checkKey, withinWindow, withinWindow)
	require.NoError(t, err)
	allowed, err := checkRedisRateLimit(ctx, redisClient, checkKey, 2, 60)
	require.NoError(t, err)
	assert.False(t, allowed, "an existing UTC timestamp inside the window must remain limited on a non-UTC host")
}

func useModelRateLimitSettings(t *testing.T, durationMinutes, totalCount, successCount int) {
	t.Helper()

	previousEnabled := setting.ModelRequestRateLimitEnabled
	previousDuration := setting.ModelRequestRateLimitDurationMinutes
	previousTotal := setting.ModelRequestRateLimitCount
	previousSuccess := setting.ModelRequestRateLimitSuccessCount

	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = durationMinutes
	setting.ModelRequestRateLimitCount = totalCount
	setting.ModelRequestRateLimitSuccessCount = successCount

	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = previousEnabled
		setting.ModelRequestRateLimitDurationMinutes = previousDuration
		setting.ModelRequestRateLimitCount = previousTotal
		setting.ModelRequestRateLimitSuccessCount = previousSuccess
	})
}

func useMemoryModelRateLimit(t *testing.T) {
	t.Helper()

	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
}

func newModelRateLimitRouter(userID int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET(
		"/model-limited",
		func(c *gin.Context) { c.Set("id", userID) },
		ModelRequestRateLimit(),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	return router
}

func performModelRateLimitRequest(router http.Handler) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/model-limited", nil))
	return recorder
}

func TestMemoryModelRateLimitReportsSuccessLimitInEnglish(t *testing.T) {
	useModelRateLimitSettings(t, 7, 0, 3)
	useMemoryModelRateLimit(t)

	router := newModelRateLimitRouter(920001)
	for range 3 {
		assert.Equal(t, http.StatusNoContent, performModelRateLimitRequest(router).Code)
	}

	limited := performModelRateLimitRequest(router)
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Contains(t, limited.Body.String(), "Rate limit reached: at most 3 requests within 7 minutes")
	assert.Contains(t, limited.Body.String(), "new_api_error")
}

func TestMemoryModelRateLimitReportsTotalLimitInEnglish(t *testing.T) {
	useModelRateLimitSettings(t, 7, 2, 1000)
	useMemoryModelRateLimit(t)

	router := newModelRateLimitRouter(920002)
	for range 2 {
		assert.Equal(t, http.StatusNoContent, performModelRateLimitRequest(router).Code)
	}

	limited := performModelRateLimitRequest(router)
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Contains(t, limited.Body.String(), "Total rate limit reached: at most 2 requests within 7 minutes, including failed ones")
}

func TestMemoryModelRateLimitTreatsZeroSuccessCountAsUnlimited(t *testing.T) {
	useModelRateLimitSettings(t, 7, 0, 0)
	useMemoryModelRateLimit(t)

	router := newModelRateLimitRouter(920003)
	for range 3 {
		assert.Equal(t, http.StatusNoContent, performModelRateLimitRequest(router).Code)
	}
}

func TestRedisModelRateLimitReportsSuccessLimitInEnglish(t *testing.T) {
	useModelRateLimitSettings(t, 7, 0, 3)
	useRateLimitMiniRedis(t)

	router := newModelRateLimitRouter(920004)
	for range 3 {
		assert.Equal(t, http.StatusNoContent, performModelRateLimitRequest(router).Code)
	}

	limited := performModelRateLimitRequest(router)
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Contains(t, limited.Body.String(), "Rate limit reached: at most 3 requests within 7 minutes")
}
