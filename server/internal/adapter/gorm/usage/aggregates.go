package usage

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/model"
)

func (Store) CountAggregateUsers(ids []int) (int64, error) {
	var count int64
	err := model.DB.Unscoped().Model(&model.User{}).Where("id IN ?", ids).Count(&count).Error
	return count, err
}

func (Store) ReadLogAggregate(q domain.AggregateScope, kind int) (domain.LogAggregate, error) {
	var result domain.LogAggregate
	sum := "COALESCE(SUM(quota), 0)"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		sum = "toString(SUM(toDecimal128(quota, 0)))"
	}
	err := model.LOG_DB.Table("logs").Select(sum+" AS quota, COUNT(*) AS request_count").
		Where("user_id IN ? AND type = ? AND created_at >= ? AND created_at <= ?", q.UserIDs, kind, *q.StartTimestamp, *q.EndTimestamp).
		Scan(&result).Error
	if err != nil {
		return domain.LogAggregate{}, err
	}
	return result, nil
}

type aggregateRow struct {
	UserID                  *int
	ModelName               *string
	CreatedAt               *int64 `gorm:"column:bucket_start"`
	Quota, Count, TokenUsed int64
}

func (Store) ReadUsageAggregate(q domain.UsageAggregateQuery, limit int) ([]domain.UsageAggregateRow, error) {
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
	rows := make([]aggregateRow, 0)
	err := model.DB.Table("quota_data").Select(strings.Join(selection, ", "), args...).
		Where("user_id IN ? AND created_at >= ? AND created_at <= ?", q.UserIDs, *q.StartTimestamp, *q.EndTimestamp).
		Group(strings.Join(grouping, ", ")).Order(strings.Join(grouping, ", ")).Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.UsageAggregateRow, len(rows))
	for i, row := range rows {
		out[i] = domain.UsageAggregateRow(row)
	}
	return out, nil
}
