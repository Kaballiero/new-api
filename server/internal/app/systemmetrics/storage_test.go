package systemmetrics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/systemmetrics"
	aggregation "github.com/QuantumNous/new-api/internal/adapter/runtime/systemmetrics"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openSQLite points the main and log databases at a fresh SQLite file for one test.
func openSQLite(t *testing.T) {
	t.Helper()
	fixture.InstallDBClock(t)
	fixture.Configure()
	oldMain, oldLog := model.DB, model.LOG_DB
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	common.SQLitePath = filepath.Join(t.TempDir(), "system.db") + "?_pragma=synchronous(OFF)"
	common.IsMasterNode = true
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	mainDB, logDB := model.DB, model.LOG_DB
	t.Cleanup(func() {
		pool, err := mainDB.DB()
		require.NoError(t, err)
		require.NoError(t, pool.Close())
		if logDB != mainDB {
			pool, err = logDB.DB()
			require.NoError(t, err)
			require.NoError(t, pool.Close())
		}
		model.DB, model.LOG_DB = oldMain, oldLog
	})
}

func TestSystemMetricsRedis(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR is required for real Redis success")
	}
	openSQLite(t)
	hotBuckets = aggregation.Buckets{}
	t.Cleanup(func() { hotBuckets = aggregation.Buckets{} })
	require.NoError(t, model.DB.AutoMigrate(&model.PerfMetric{}))
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("perf_metrics_setting"), map[string]string{"enabled": "true", "bucket_time": "hour"}))
	client := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("TEST_REDIS_PASSWORD"), MaxRetries: -1})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Ping(ctx).Err())
	oldEnabled, oldClient := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() { common.RedisEnabled, common.RDB = oldEnabled, oldClient })
	sample := domain.Sample{Model: fmt.Sprintf("wave7h_%d", time.Now().UnixNano()), LatencyMs: 300, TtftMs: 20, HasTtft: true, Success: true, OutputTokens: 30, GenerationMs: 200}
	// Register targeted cleanup before Record, including a possible hour boundary.
	keys := []string{fmt.Sprintf("perf:%s:default:%d", sample.Model, bucketStart(time.Now().Unix())), fmt.Sprintf("perf:%s:default:%d", sample.Model, bucketStart(time.Now().Add(time.Hour).Unix()))}
	defer client.Del(context.Background(), keys...)
	Record(sample)
	key := ""
	hotBuckets.Range(func(raw, _ any) bool {
		k := raw.(domain.MetricBucketKey)
		key = fmt.Sprintf("perf:%s:%s:%d", k.Model, k.Group, k.BucketTs)
		return true
	})
	require.NotEmpty(t, key)
	values, err := client.HGetAll(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"req": "1", "ok": "1", "lat": "300", "ttft": "20", "ttft_n": "1", "out": "30", "gen_ms": "200"}, values)
	ttl, err := client.TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0))
	assert.LessOrEqual(t, ttl, time.Hour)
	// Query still reads only durable SQL and local buckets, even with a Redis hash.
	hotBuckets = aggregation.Buckets{}
	result, err := Query(domain.QueryParams{Model: sample.Model, Hours: 24})
	require.NoError(t, err)
	assert.Empty(t, result.Groups)
}

func TestSystemMetricsPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is required")
	}
	db := pgfixture.Open(t, dsn)
	previous, previousType, previousColumn := model.DB, common.MainDatabaseType(), model.GroupColumn
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	model.GroupColumn = `"group"`
	t.Cleanup(func() {
		model.DB = previous
		common.SetMainDatabaseType(previousType)
		model.GroupColumn = previousColumn
	})
	var version string
	require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	t.Logf("database version: %s", version)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	row := model.PerfMetric{ModelName: "metrics-pg", Group: "group", BucketTs: 100, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 300, TtftSumMs: 20, TtftCount: 1, OutputTokens: 30, GenerationMs: 200}
	require.NoError(t, storage.UpsertPerfMetric(&row))
	row.Id = 0
	require.NoError(t, storage.UpsertPerfMetric(&row))
	rows, err := storage.GetPerfMetrics("metrics-pg", "group", 1, 200)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(2), rows[0].RequestCount)
	assert.Equal(t, int64(600), rows[0].TotalLatencyMs)
	assert.Equal(t, int64(40), rows[0].TtftSumMs)
	assert.Equal(t, int64(2), rows[0].TtftCount)
	assert.Equal(t, int64(60), rows[0].OutputTokens)
	assert.Equal(t, int64(400), rows[0].GenerationMs)
	summary, err := storage.GetPerfMetricsSummaryBucketsAll(1, 200, nil)
	require.NoError(t, err)
	require.Len(t, summary, 1)
	assert.Equal(t, int64(2), summary[0].RequestCount)
	empty, err := storage.GetPerfMetricsSummaryBucketsAll(1, 200, []string{})
	require.NoError(t, err)
	assert.Nil(t, empty)
	filtered, err := storage.GetPerfMetricsSummaryBucketsAll(1, 200, []string{"other"})
	require.NoError(t, err)
	assert.Empty(t, filtered)
	require.NoError(t, storage.DeletePerfMetricsBefore(100))
	rows, err = storage.GetPerfMetrics("metrics-pg", "group", 1, 200)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	require.NoError(t, storage.DeletePerfMetricsBefore(101))
	rows, err = storage.GetPerfMetrics("metrics-pg", "group", 1, 200)
	require.NoError(t, err)
	assert.Empty(t, rows)
}
