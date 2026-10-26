package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
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
	UserIDs        []int  `json:"user_ids"`
	StartTimestamp *int64 `json:"start_timestamp"`
	EndTimestamp   *int64 `json:"end_timestamp"`
}

func (q *AggregateScope) Validate() error {
	if len(q.UserIDs) == 0 || len(q.UserIDs) > MaxAggregateUsers {
		return fmt.Errorf("%w: user_ids must contain 1..100 positive integers", ErrInvalidAggregate)
	}
	for _, id := range q.UserIDs {
		if id <= 0 || int64(id) > MaxAggregateInteger {
			return fmt.Errorf("%w: user_ids must be positive safe integers", ErrInvalidAggregate)
		}
	}
	if q.StartTimestamp == nil || q.EndTimestamp == nil || *q.StartTimestamp < 0 || *q.EndTimestamp < *q.StartTimestamp || *q.EndTimestamp > MaxAggregateInteger {
		return fmt.Errorf("%w: inclusive timestamps must be nonnegative safe integers in order", ErrInvalidAggregate)
	}
	q.UserIDs = slices.Clone(q.UserIDs)
	slices.Sort(q.UserIDs)
	q.UserIDs = slices.Compact(q.UserIDs)
	return nil
}

func (q AggregateScope) requireUsers() error {
	var count int64
	if err := DB.Unscoped().Model(&User{}).Where("id IN ?", q.UserIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(q.UserIDs)) {
		return ErrAggregateUserNotFound
	}
	return nil
}

// LogAggregate keeps quota as gross consumption and request_count as consume
// rows only; refund_quota is the separately reported refunded amount.
type LogAggregate struct {
	Quota        int64 `json:"quota"`
	RefundQuota  int64 `json:"refund_quota"`
	RequestCount int64 `json:"request_count"`
}

func GetLogAggregate(q AggregateScope) (LogAggregate, error) {
	var result LogAggregate
	if err := q.Validate(); err != nil {
		return result, err
	}
	if err := q.requireUsers(); err != nil {
		return result, err
	}
	selection := "COALESCE(SUM(CASE WHEN type = ? THEN quota ELSE 0 END), 0) AS quota, " +
		"COALESCE(SUM(CASE WHEN type = ? THEN quota ELSE 0 END), 0) AS refund_quota, " +
		"COALESCE(SUM(CASE WHEN type = ? THEN 1 ELSE 0 END), 0) AS request_count"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		selection = "toString(SUM(toDecimal128(if(type = ?, quota, 0), 0))) AS quota, " +
			"toString(SUM(toDecimal128(if(type = ?, quota, 0), 0))) AS refund_quota, " +
			"countIf(type = ?) AS request_count"
	}
	err := LOG_DB.Table("logs").Select(selection, LogTypeConsume, LogTypeRefund, LogTypeConsume).
		Where("user_id IN ? AND type IN ? AND created_at >= ? AND created_at <= ?", q.UserIDs, []int{LogTypeConsume, LogTypeRefund}, *q.StartTimestamp, *q.EndTimestamp).
		Scan(&result).Error
	if err != nil {
		return LogAggregate{}, err
	}
	if err := validateAggregateIntegers(result.Quota, result.RefundQuota, result.RequestCount); err != nil {
		return LogAggregate{}, err
	}
	return result, nil
}

type UsageAggregateQuery struct {
	AggregateScope
	GroupBy     string `json:"group_by"`
	Granularity string `json:"granularity"`
}

type UsageAggregateRow struct {
	UserID    *int    `json:"user_id,omitempty"`
	ModelName *string `json:"model_name,omitempty"`
	CreatedAt *int64  `json:"created_at,omitempty" gorm:"column:bucket_start"`
	Quota     int64   `json:"quota"`
	Count     int64   `json:"count"`
	TokenUsed int64   `json:"token_used"`
}

func GetUsageAggregate(q UsageAggregateQuery) ([]UsageAggregateRow, error) {
	if err := q.AggregateScope.Validate(); err != nil {
		return nil, err
	}
	if !IsValidQuotaGranularity(q.Granularity) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAggregate, ErrInvalidQuotaGranularity)
	}
	if q.Granularity == GranularityMonth && len(quotaMonthBoundaries(*q.StartTimestamp, *q.EndTimestamp))-1 > MaxBucketedMonths {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAggregate, ErrQuotaMonthSpanExceeded)
	}
	selection := []string{"SUM(quota) AS quota", "SUM(count) AS count", "SUM(token_used) AS token_used"}
	grouping := []string{}
	var args []any
	if q.GroupBy == "time" || q.GroupBy == "time_model" {
		bucket, bucketArgs, err := quotaBucketExpression(q.Granularity, *q.StartTimestamp, *q.EndTimestamp)
		if err != nil {
			return nil, err
		}
		selection = append(selection, bucket+" AS bucket_start")
		grouping = append(grouping, "bucket_start")
		args = bucketArgs
	}
	if q.GroupBy == "model" || q.GroupBy == "time_model" {
		modelKey := "model_name COLLATE BINARY"
		switch common.MainDatabaseType() {
		case common.DatabaseTypeMySQL:
			modelKey = "CAST(model_name AS BINARY)"
		case common.DatabaseTypePostgreSQL:
			modelKey = `model_name COLLATE "C"`
		}
		selection = append(selection, modelKey+" AS model_name")
		grouping = append(grouping, modelKey)
	}
	if q.GroupBy == "user" {
		selection = append(selection, "user_id")
		grouping = append(grouping, "user_id")
	}
	if len(grouping) == 0 {
		return nil, fmt.Errorf("%w: group_by must be time, model, time_model or user", ErrInvalidAggregate)
	}
	if err := q.requireUsers(); err != nil {
		return nil, err
	}
	rows := make([]UsageAggregateRow, 0)
	err := DB.Table("quota_data").Select(strings.Join(selection, ", "), args...).
		Where("user_id IN ? AND created_at >= ? AND created_at <= ?", q.UserIDs, *q.StartTimestamp, *q.EndTimestamp).
		Group(strings.Join(grouping, ", ")).Order(strings.Join(grouping, ", ")).Limit(MaxAggregateRows + 1).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxAggregateRows {
		return nil, ErrAggregateTooLarge
	}
	for _, row := range rows {
		if err := validateAggregateIntegers(row.Quota, row.Count, row.TokenUsed); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func validateAggregateIntegers(values ...int64) error {
	for _, value := range values {
		if value < -MaxAggregateInteger || value > MaxAggregateInteger {
			return ErrAggregateOverflow
		}
	}
	return nil
}
