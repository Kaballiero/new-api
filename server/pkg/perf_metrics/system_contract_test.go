package perfmetrics

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
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
	sf.Run(t, "TestSystemMetricsContract", "metrics", "testdata/system", names, runSystemMetrics)
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
	hotBuckets = sync.Map{}
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
		sample := Sample{Model: "fixture", Group: "active", LatencyMs: 300, TtftMs: 20, HasTtft: true, Success: true, OutputTokens: 30, GenerationMs: 200}
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
				require.NoError(t, model.UpsertPerfMetric(&model.PerfMetric{ModelName: "fixture",
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
				params := QueryParams{Model: "fixture", Hours: 24}
				if name == "query_inactive" {
					params.Group = "other"
				}
				if name == "query_hours_bounds" {
					params.Hours = 721
					for i, age := range []int64{25 * 3600, 721 * 3600} {
						require.NoError(t, model.UpsertPerfMetric(&model.PerfMetric{ModelName: "fixture", Group: "active", BucketTs: fixture.Epoch - age, RequestCount: 1, TotalLatencyMs: int64(100 + i)}))
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
			result, err := Query(QueryParams{Model: "fixture", Hours: 24})
			require.NoError(t, err)
			extra["after_flush"] = result
		case "flush_empty_old":
			time.Sleep(25 * time.Hour)
			flushCompletedBuckets()
			flushCompletedBuckets()
		case "retention_off", "retention_positive":
			require.NoError(t, model.UpsertPerfMetric(&model.PerfMetric{ModelName: "old", Group: "default", BucketTs: fixture.Epoch - 86401, RequestCount: 1}))
			days := 0
			if name == "retention_positive" {
				days = 1
			}
			cleanupExpiredMetrics(days)
		case "flush_loop_disabled", "flush_loop_interval":
			if name == "flush_loop_disabled" {
				bucket := &atomicBucket{}
				bucket.add(sample)
				hotBuckets.Store(bucketKey{model: "fixture", group: "active", bucketTs: fixture.Epoch - 3600}, bucket)
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
			k := key.(bucketKey)
			v := value.(*atomicBucket).snapshot()
			rows = append(rows, map[string]any{"model": k.model, "group": k.group, "bucket": k.bucketTs,
				"request": v.requestCount, "success": v.successCount, "latency": v.totalLatencyMs,
				"ttft": v.ttftSumMs, "ttft_count": v.ttftCount, "output": v.outputTokens, "generation": v.generationMs})
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
