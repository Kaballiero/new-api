package domain

import (
	"errors"
	"slices"
)

const (
	MaxAggregateUsers         = 100
	MaxAggregateRows          = 10000
	MaxAggregateInteger int64 = 1<<53 - 1
)

var (
	ErrInvalidAggregate      = errors.New("invalid aggregate request")
	ErrAggregateUserNotFound = errors.New("aggregate user not found")
	ErrAggregateOverflow     = errors.New("aggregate exceeds the exact JSON integer range")
	ErrAggregateTooLarge     = errors.New("aggregate response exceeds the limit")
)

type AggregateScope struct {
	UserIDs        []int
	StartTimestamp *int64
	EndTimestamp   *int64
}

func (q *AggregateScope) Validate() error {
	if len(q.UserIDs) == 0 || len(q.UserIDs) > MaxAggregateUsers {
		return ErrUsageAggregateUserCount
	}
	for _, id := range q.UserIDs {
		if id <= 0 || int64(id) > MaxAggregateInteger {
			return ErrUsageAggregateUserIDs
		}
	}
	if q.StartTimestamp == nil ||
		q.EndTimestamp == nil ||
		*q.StartTimestamp < 0 ||
		*q.EndTimestamp < *q.StartTimestamp ||
		*q.EndTimestamp > MaxAggregateInteger {
		return ErrUsageAggregateTimes
	}
	q.UserIDs = slices.Clone(q.UserIDs)
	slices.Sort(q.UserIDs)
	q.UserIDs = slices.Compact(q.UserIDs)
	return nil
}

type UsageAggregateQuery struct {
	AggregateScope
	GroupBy     string
	Granularity string
}

type UsageAggregateRow struct {
	UserID    *int
	ModelName *string
	CreatedAt *int64
	Quota     int64
	Count     int64
	TokenUsed int64
}

func ValidateAggregateIntegers(values ...int64) error {
	for _, value := range values {
		if value < -MaxAggregateInteger || value > MaxAggregateInteger {
			return ErrAggregateOverflow
		}
	}
	return nil
}

func (q *UsageAggregateQuery) Validate() error {
	if err := q.AggregateScope.Validate(); err != nil {
		return err
	}
	if !IsValidQuotaGranularity(q.Granularity) {
		return ErrUsageAggregateGranularity
	}
	if q.Granularity == GranularityMonth && len(QuotaMonthBoundaries(*q.StartTimestamp, *q.EndTimestamp))-1 > MaxBucketedMonths {
		return ErrUsageAggregateMonths
	}
	switch q.GroupBy {
	case "time", "model", "time_model", "user":
		return nil
	}
	return ErrUsageAggregateGroup
}

type LogAggregate struct {
	Quota, RequestCount int64
}
