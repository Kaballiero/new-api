package systemmetrics

import (
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/systemmetrics"
	redis "github.com/QuantumNous/new-api/internal/adapter/redis/systemmetrics"
	aggregation "github.com/QuantumNous/new-api/internal/adapter/runtime/systemmetrics"
	"github.com/QuantumNous/new-api/internal/system/domain"

	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

var hotBuckets aggregation.Buckets

// seriesSchema is a stable client cache/schema marker. Do not change it when
// hiding fields or making response-only privacy hardening changes.
const seriesSchema = "dbcd0a3c01b55203"

func Init() {
	go flushLoop()
}

func Record(sample domain.Sample) {
	setting := perf_metrics_setting.GetSetting()
	if !setting.Enabled || sample.Model == "" {
		return
	}
	if sample.Group == "" {
		sample.Group = "default"
	}
	if sample.LatencyMs < 0 {
		sample.LatencyMs = 0
	}

	key := domain.MetricBucketKey{
		Model:    sample.Model,
		Group:    sample.Group,
		BucketTs: bucketStart(time.Now().Unix()),
	}
	actual, _ := hotBuckets.LoadOrStore(key, &aggregation.Bucket{})
	actual.(*aggregation.Bucket).Add(sample)
	redis.Record(key, sample)
}

func Query(params domain.QueryParams) (domain.QueryResult, error) {
	if params.Hours <= 0 {
		params.Hours = 24
	}
	if params.Hours > 24*30 {
		params.Hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(params.Hours)*3600

	merged := map[domain.MetricBucketKey]domain.MetricCounters{}
	rows, err := storage.GetPerfMetrics(params.Model, params.Group, startTs, endTs)
	if err != nil {
		return domain.QueryResult{}, err
	}
	for _, row := range rows {
		mergeCounters(merged, domain.MetricBucketKey{
			Model:    row.ModelName,
			Group:    row.Group,
			BucketTs: row.BucketTs,
		}, domain.MetricCounters{
			RequestCount:   row.RequestCount,
			SuccessCount:   row.SuccessCount,
			TotalLatencyMs: row.TotalLatencyMs,
			TtftSumMs:      row.TtftSumMs,
			TtftCount:      row.TtftCount,
			OutputTokens:   row.OutputTokens,
			GenerationMs:   row.GenerationMs,
		})
	}

	hotBuckets.Range(func(key, value any) bool {
		k := key.(domain.MetricBucketKey)
		if k.Model != params.Model || k.BucketTs < startTs || k.BucketTs > endTs {
			return true
		}
		if params.Group != "" && k.Group != params.Group {
			return true
		}
		mergeCounters(merged, k, value.(*aggregation.Bucket).Snapshot())
		return true
	})

	return buildQueryResult(params.Model, merged), nil
}

func QuerySummaryAll(hours int, groups []string) (domain.SummaryAllResult, error) {
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600
	allowedGroups := allowedGroupSet(groups)

	rows, err := storage.GetPerfMetricsSummaryBucketsAll(startTs, endTs, groups)
	if err != nil {
		return domain.SummaryAllResult{}, err
	}

	totals := map[string]domain.MetricCounters{}
	modelBuckets := map[string]map[int64]domain.MetricCounters{}
	for _, row := range rows {
		value := domain.MetricCounters{
			RequestCount:   row.RequestCount,
			SuccessCount:   row.SuccessCount,
			TotalLatencyMs: row.TotalLatencyMs,
			OutputTokens:   row.OutputTokens,
			GenerationMs:   row.GenerationMs,
		}
		mergeModelTotals(totals, row.ModelName, value)
		mergeModelBucket(modelBuckets, row.ModelName, row.BucketTs, value)
	}

	hotBuckets.Range(func(key, value any) bool {
		k := key.(domain.MetricBucketKey)
		if k.BucketTs < startTs || k.BucketTs > endTs {
			return true
		}
		if allowedGroups != nil {
			if _, ok := allowedGroups[k.Group]; !ok {
				return true
			}
		}
		snap := value.(*aggregation.Bucket).Snapshot()
		if snap.RequestCount == 0 {
			return true
		}
		mergeModelTotals(totals, k.Model, snap)
		mergeModelBucket(modelBuckets, k.Model, k.BucketTs, snap)
		return true
	})

	models := make([]domain.ModelSummary, 0, len(totals))
	for name, total := range totals {
		if total.RequestCount == 0 {
			continue
		}
		avgLatency := total.TotalLatencyMs / total.RequestCount
		successRate := float64(total.SuccessCount) / float64(total.RequestCount) * 100
		avgTps := 0.0
		if total.GenerationMs > 0 {
			avgTps = float64(total.OutputTokens) / (float64(total.GenerationMs) / 1000.0)
		}
		models = append(models, domain.ModelSummary{
			ModelName:           name,
			AvgLatencyMs:        avgLatency,
			SuccessRate:         math.Round(successRate*100) / 100,
			AvgTps:              math.Round(avgTps*100) / 100,
			RecentSuccessSeries: recentSuccessSeries(modelBuckets[name]),
			RequestCount:        total.RequestCount,
		})
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].RequestCount > models[j].RequestCount
	})

	return domain.SummaryAllResult{Models: models}, nil
}

func mergeModelTotals(totals map[string]domain.MetricCounters, modelName string, value domain.MetricCounters) {
	if value.RequestCount == 0 {
		return
	}
	current := totals[modelName]
	current.RequestCount += value.RequestCount
	current.SuccessCount += value.SuccessCount
	current.TotalLatencyMs += value.TotalLatencyMs
	current.TtftSumMs += value.TtftSumMs
	current.TtftCount += value.TtftCount
	current.OutputTokens += value.OutputTokens
	current.GenerationMs += value.GenerationMs
	totals[modelName] = current
}

func mergeModelBucket(modelBuckets map[string]map[int64]domain.MetricCounters, modelName string, bucketTs int64, value domain.MetricCounters) {
	if value.RequestCount == 0 {
		return
	}
	if _, ok := modelBuckets[modelName]; !ok {
		modelBuckets[modelName] = map[int64]domain.MetricCounters{}
	}
	current := modelBuckets[modelName][bucketTs]
	current.RequestCount += value.RequestCount
	current.SuccessCount += value.SuccessCount
	current.TotalLatencyMs += value.TotalLatencyMs
	current.TtftSumMs += value.TtftSumMs
	current.TtftCount += value.TtftCount
	current.OutputTokens += value.OutputTokens
	current.GenerationMs += value.GenerationMs
	modelBuckets[modelName][bucketTs] = current
}

func recentSuccessSeries(buckets map[int64]domain.MetricCounters) []domain.SuccessRatePoint {
	if len(buckets) == 0 {
		return nil
	}
	hourly := map[int64]domain.MetricCounters{}
	for ts, value := range buckets {
		hourTs := ts - ts%3600
		merged := hourly[hourTs]
		merged.RequestCount += value.RequestCount
		merged.SuccessCount += value.SuccessCount
		hourly[hourTs] = merged
	}
	timestamps := make([]int64, 0, len(hourly))
	for hourTs, value := range hourly {
		if value.RequestCount == 0 {
			continue
		}
		timestamps = append(timestamps, hourTs)
	}
	if len(timestamps) == 0 {
		return nil
	}
	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})
	points := make([]domain.SuccessRatePoint, 0, len(timestamps))
	for _, hourTs := range timestamps {
		points = append(points, domain.SuccessRatePoint{
			Ts:          hourTs,
			SuccessRate: math.Round(successRate(hourly[hourTs])*100) / 100,
		})
	}
	return points
}

func allowedGroupSet(groups []string) map[string]struct{} {
	if groups == nil {
		return nil
	}
	allowed := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		allowed[group] = struct{}{}
	}
	return allowed
}

func bucketStart(ts int64) int64 {
	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	return ts - (ts % bucketSeconds)
}

func mergeCounters(merged map[domain.MetricBucketKey]domain.MetricCounters, key domain.MetricBucketKey, value domain.MetricCounters) {
	if value.RequestCount == 0 {
		return
	}
	current := merged[key]
	current.RequestCount += value.RequestCount
	current.SuccessCount += value.SuccessCount
	current.TotalLatencyMs += value.TotalLatencyMs
	current.TtftSumMs += value.TtftSumMs
	current.TtftCount += value.TtftCount
	current.OutputTokens += value.OutputTokens
	current.GenerationMs += value.GenerationMs
	merged[key] = current
}

func buildQueryResult(modelName string, merged map[domain.MetricBucketKey]domain.MetricCounters) domain.QueryResult {
	groupBuckets := map[string]map[int64]domain.MetricCounters{}
	for key, value := range merged {
		if value.RequestCount == 0 {
			continue
		}
		if _, ok := groupBuckets[key.Group]; !ok {
			groupBuckets[key.Group] = map[int64]domain.MetricCounters{}
		}
		groupBuckets[key.Group][key.BucketTs] = value
	}

	groups := make([]string, 0, len(groupBuckets))
	for group := range groupBuckets {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	results := make([]domain.GroupResult, 0, len(groups))
	for _, group := range groups {
		buckets := groupBuckets[group]
		timestamps := make([]int64, 0, len(buckets))
		for ts := range buckets {
			timestamps = append(timestamps, ts)
		}
		sort.Slice(timestamps, func(i, j int) bool {
			return timestamps[i] < timestamps[j]
		})

		total := domain.MetricCounters{}
		series := make([]domain.BucketPoint, 0, len(timestamps))
		for _, ts := range timestamps {
			value := buckets[ts]
			total.RequestCount += value.RequestCount
			total.SuccessCount += value.SuccessCount
			total.TotalLatencyMs += value.TotalLatencyMs
			total.TtftSumMs += value.TtftSumMs
			total.TtftCount += value.TtftCount
			total.OutputTokens += value.OutputTokens
			total.GenerationMs += value.GenerationMs
			series = append(series, bucketPoint(ts, value))
		}

		results = append(results, domain.GroupResult{
			Group:        group,
			AvgTtftMs:    avg(total.TtftSumMs, total.TtftCount),
			AvgLatencyMs: avg(total.TotalLatencyMs, total.RequestCount),
			SuccessRate:  successRate(total),
			AvgTps:       avgTps(total),
			Series:       series,
		})
	}

	return domain.QueryResult{
		ModelName:    modelName,
		SeriesSchema: seriesSchema,
		Groups:       results,
	}
}

func bucketPoint(ts int64, value domain.MetricCounters) domain.BucketPoint {
	return domain.BucketPoint{
		Ts:           ts,
		AvgTtftMs:    avg(value.TtftSumMs, value.TtftCount),
		AvgLatencyMs: avg(value.TotalLatencyMs, value.RequestCount),
		SuccessRate:  successRate(value),
		AvgTps:       avgTps(value),
	}
}

func avg(sum int64, count int64) int64 {
	if count <= 0 {
		return 0
	}
	return sum / count
}

func successRate(value domain.MetricCounters) float64 {
	if value.RequestCount <= 0 {
		return 0
	}
	return float64(value.SuccessCount) / float64(value.RequestCount) * 100
}

func avgTps(value domain.MetricCounters) float64 {
	if value.OutputTokens <= 0 || value.GenerationMs <= 0 {
		return 0
	}
	return float64(value.OutputTokens) / (float64(value.GenerationMs) / 1000)
}
