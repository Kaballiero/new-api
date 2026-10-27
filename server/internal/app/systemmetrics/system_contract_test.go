package systemmetrics

import (
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/systemmetrics"
	aggregation "github.com/QuantumNous/new-api/internal/adapter/runtime/systemmetrics"
	"github.com/QuantumNous/new-api/internal/system/domain"

	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	sf "github.com/QuantumNous/new-api/internal/contracttest/systemfixture"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func init() { fixture.RegisterUpdate(); fixture.RejectCandidateUpdate() }

func TestSystemMetricsContract(t *testing.T) {
	names := []string{"record_success", "record_failure", "record_zero", "record_negative",
		"record_default_group", "record_disabled", "record_empty_model", "record_redis",
		"query_empty", "query_local", "query_mixed", "query_inactive", "query_hours_bounds",
		"summary_groups_nil", "summary_groups_empty", "summary_groups_filter", "flush_current",
		"flush_completed", "flush_error_restore", "flush_empty_old", "retention_off",
		"retention_positive", "flush_loop_disabled", "flush_loop_interval"}
	slices.Sort(names)
	sf.Run(t, "TestSystemMetricsContract", "metrics", "../../../pkg/perf_metrics/testdata/system", names, runSystemMetrics)
}

type systemRedisHook struct {
	calls [][]string
}

func (h *systemRedisHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (h *systemRedisHook) AfterProcess(context.Context, redis.Cmder) error { return nil }
func (h *systemRedisHook) BeforeProcessPipeline(ctx context.Context, commands []redis.Cmder) (context.Context, error) {
	for _, cmd := range commands {
		values := []string{}
		for _, arg := range cmd.Args() {
			values = append(values, fmt.Sprint(arg))
		}
		h.calls = append(h.calls, values)
	}
	// Returning a deterministic error prevents any socket dial, including the success-shaped command capture.
	return ctx, errors.New("fixture redis unavailable")
}
func (h *systemRedisHook) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func runSystemMetrics(t *testing.T, name string) any {
	sf.Open(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Setup{}, &model.Option{}, &model.SystemInstance{}, &model.PerfMetric{}))
	hotBuckets = aggregation.Buckets{}
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("perf_metrics_setting"),
		map[string]string{"enabled": "true", "bucket_time": "hour", "retention_days": "0",
			"flush_interval": "1"}))
	var output any
	synctest.Test(t, func(t *testing.T) {
		extra := map[string]any{}
		hook := &systemRedisHook{}
		if strings.Contains(name, "redis") {
			common.RedisEnabled = true
			common.RDB = redis.NewClient(&redis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
			common.RDB.AddHook(hook)
			defer common.RDB.Close()
		}
		sample := domain.Sample{Model: "fixture", Group: "active", LatencyMs: 300, TtftMs: 20, HasTtft: true, Success: true, OutputTokens: 30, GenerationMs: 200}
		switch name {
		case "record_failure":
			sample.Success = false
		case "record_zero":
			sample.LatencyMs = 0
			sample.TtftMs = 0
			sample.OutputTokens = 0
			sample.GenerationMs = 0
		case "record_negative":
			sample.LatencyMs = -1
			sample.TtftMs = -1
			sample.OutputTokens = -1
			sample.GenerationMs = -1
		case "record_default_group":
			sample.Group = ""
		case "record_disabled", "flush_loop_disabled":
			require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("perf_metrics_setting"), map[string]string{"enabled": "false"}))
		case "record_empty_model":
			sample.Model = ""
		}
		if name != "query_empty" {
			Record(sample)
		}
		if strings.HasPrefix(name, "query_") || strings.HasPrefix(name, "summary_") {
			if name == "query_mixed" || strings.HasPrefix(name, "summary_") {
				require.NoError(t, storage.UpsertPerfMetric(&model.PerfMetric{ModelName: "fixture",
					Group: "active", BucketTs: fixture.Epoch - 3600, RequestCount: 2, SuccessCount: 1,
					TotalLatencyMs: 200, TtftSumMs: 40, TtftCount: 1, OutputTokens: 20, GenerationMs: 1000}))
			}
			if strings.HasPrefix(name, "summary_") {
				groups := []string(nil)
				if name == "summary_groups_empty" {
					groups = []string{}
				}
				if name == "summary_groups_filter" {
					groups = []string{"other"}
				}
				result, err := QuerySummaryAll(24, groups)
				require.NoError(t, err)
				extra["query"] = result
			} else {
				params := domain.QueryParams{Model: "fixture", Hours: 24}
				if name == "query_inactive" {
					params.Group = "other"
				}
				if name == "query_hours_bounds" {
					params.Hours = 721
					for i, age := range []int64{25 * 3600, 721 * 3600} {
						require.NoError(t, storage.UpsertPerfMetric(&model.PerfMetric{ModelName: "fixture", Group: "active", BucketTs: fixture.Epoch - age, RequestCount: 1, TotalLatencyMs: int64(100 + i)}))
					}
				}
				result, err := Query(params)
				require.NoError(t, err)
				extra["query"] = result
			}
		}
		switch name {
		case "flush_current":
			flushCompletedBuckets()
		case "flush_completed", "flush_error_restore":
			time.Sleep(time.Hour)
			if name == "flush_error_restore" {
				require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("metrics-upsert-fault", func(db *gorm.DB) {
					if db.Statement.Table == "perf_metrics" {
						db.AddError(errors.New("fixture metric upsert failed"))
					}
				}))
			}
			flushCompletedBuckets()
			result, err := Query(domain.QueryParams{Model: "fixture", Hours: 24})
			require.NoError(t, err)
			extra["after_flush"] = result
		case "flush_empty_old":
			time.Sleep(25 * time.Hour)
			flushCompletedBuckets()
			flushCompletedBuckets()
		case "retention_off", "retention_positive":
			require.NoError(t, storage.UpsertPerfMetric(&model.PerfMetric{ModelName: "old", Group: "default", BucketTs: fixture.Epoch - 86401, RequestCount: 1}))
			days := 0
			if name == "retention_positive" {
				days = 1
			}
			cleanupExpiredMetrics(days)
		case "flush_loop_disabled", "flush_loop_interval":
			if name == "flush_loop_disabled" {
				bucket := &aggregation.Bucket{}
				bucket.Add(sample)
				hotBuckets.Store(domain.MetricBucketKey{Model: "fixture", Group: "active", BucketTs: fixture.Epoch - 3600}, bucket)
			} else {
				time.Sleep(time.Hour)
			}
			startedAt := time.Now()
			var writes int
			require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("metrics-stop-loop", func(db *gorm.DB) {
				if db.Statement.Table == "perf_metrics" {
					writes++
					_ = db.Rollback().Error
					panic("fixture-stop-metrics-loop")
				}
			}))
			if name == "flush_loop_disabled" {
				go func() {
					time.Sleep(61 * time.Second)
					require.Zero(t, writes)
					require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("perf_metrics_setting"),
						map[string]string{"enabled": "true", "flush_interval": "2"}))
				}()
			}
			func() { defer func() { require.Equal(t, "fixture-stop-metrics-loop", recover()) }(); flushLoop() }()
			extra["writes"] = writes
			extra["elapsed_seconds"] = time.Since(startedAt).Seconds()
			if name == "flush_loop_disabled" {
				assert.Equal(t, float64(120), time.Since(startedAt).Seconds())
			} else {
				assert.Equal(t, float64(60), time.Since(startedAt).Seconds())
			}

		}
		rows := []map[string]any{}
		hotBuckets.Range(func(key, value any) bool {
			k := key.(domain.MetricBucketKey)
			v := value.(*aggregation.Bucket).Snapshot()
			rows = append(rows, map[string]any{"model": k.Model, "group": k.Group, "bucket": k.BucketTs,
				"request": v.RequestCount, "success": v.SuccessCount, "latency": v.TotalLatencyMs,
				"ttft": v.TtftSumMs, "ttft_count": v.TtftCount, "output": v.OutputTokens, "generation": v.GenerationMs})
			return true
		})
		assert.LessOrEqual(t, len(rows), 1)
		state := sf.State(t)
		state["hot"] = rows
		state["extra"] = extra
		state["redis_commands"] = hook.calls
		state["enabled"] = perf_metrics_setting.GetSetting().Enabled
		output = sf.Canonical(t, state)
	})
	return output
}

// TestSystemMetricsRedis exercises successful commands on a real disposable Redis.
// Existing golden command capture deliberately returns an error before dialing.
func TestSystemMetricsRedis(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR is required for real Redis success")
	}
	sf.Open(t)
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
