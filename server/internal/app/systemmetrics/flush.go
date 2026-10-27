package systemmetrics

import (
	aggregation "github.com/QuantumNous/new-api/internal/adapter/runtime/systemmetrics"
	"github.com/QuantumNous/new-api/internal/system/domain"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/systemmetrics/record"

	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/systemmetrics"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

func flushLoop() {
	for {
		interval := perf_metrics_setting.GetFlushIntervalMinutes()
		time.Sleep(time.Duration(interval) * time.Minute)
		setting := perf_metrics_setting.GetSetting()
		if !setting.Enabled {
			continue
		}
		flushCompletedBuckets()
		cleanupExpiredMetrics(setting.RetentionDays)
	}
}

func flushCompletedBuckets() {
	currentBucket := bucketStart(time.Now().Unix())
	hotBuckets.Range(func(key, value any) bool {
		k := key.(domain.MetricBucketKey)
		if k.BucketTs >= currentBucket {
			return true
		}

		bucket := value.(*aggregation.Bucket)
		drained := bucket.Drain()
		if drained.RequestCount == 0 {
			deleteOldEmptyBucket(k, key)
			return true
		}

		err := storage.UpsertPerfMetric(&record.PerfMetric{
			ModelName:      k.Model,
			Group:          k.Group,
			BucketTs:       k.BucketTs,
			RequestCount:   drained.RequestCount,
			SuccessCount:   drained.SuccessCount,
			TotalLatencyMs: drained.TotalLatencyMs,
			TtftSumMs:      drained.TtftSumMs,
			TtftCount:      drained.TtftCount,
			OutputTokens:   drained.OutputTokens,
			GenerationMs:   drained.GenerationMs,
		})
		if err != nil {
			bucket.Restore(drained)
			common.SysError(fmt.Sprintf("failed to flush perf metric bucket model=%s group=%s bucket=%d: %s", k.Model, k.Group, k.BucketTs, err.Error()))
			return true
		}

		deleteOldEmptyBucket(k, key)
		return true
	})
}

func deleteOldEmptyBucket(k domain.MetricBucketKey, rawKey any) {
	if k.BucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) {
		hotBuckets.Delete(rawKey)
	}
}

func cleanupExpiredMetrics(retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if err := storage.DeletePerfMetricsBefore(cutoff); err != nil {
		common.SysError("failed to cleanup expired perf metrics: " + err.Error())
	}
}
