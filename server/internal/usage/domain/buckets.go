package domain

import (
	"errors"
	"time"
)

const (
	GranularityHour  = "hour"
	GranularityDay   = "day"
	GranularityWeek  = "week"
	GranularityMonth = "month"

	MaxBucketedMonths = 120
)

var (
	ErrInvalidQuotaGranularity = errors.New("granularity must be one of hour, day, week, month")
	ErrQuotaMonthSpanExceeded  = errors.New("granularity month supports at most 120 months per request")
	ErrInvalidQuotaGrouping    = errors.New("bucketed quota query requires an explicit grouping")
)

// QuotaBucketGrouping names the dimension set a bucketed query reports. Each
// legacy endpoint reports a different one, so the caller states it instead of
// letting the query infer it from whichever filters happen to be set.
type QuotaBucketGrouping string

const (
	QuotaBucketByModel        QuotaBucketGrouping = "model"
	QuotaBucketByUserAndModel QuotaBucketGrouping = "user_model"
	QuotaBucketByUser         QuotaBucketGrouping = "user"
)

func IsValidQuotaGranularity(granularity string) bool {
	switch granularity {
	case GranularityHour, GranularityDay, GranularityWeek, GranularityMonth:
		return true
	}
	return false
}

func QuotaMonthBoundaries(startTime int64, endTime int64) []int64 {
	start := time.Unix(startTime, 0).UTC()
	boundary := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	boundaries := []int64{boundary.Unix()}
	for len(boundaries)-1 <= MaxBucketedMonths {
		boundary = boundary.AddDate(0, 1, 0)
		boundaries = append(boundaries, boundary.Unix())
		if boundary.Unix() > endTime {
			break
		}
	}
	return boundaries
}
