package usage

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/getapi_aggregate"
	"github.com/QuantumNous/new-api/model"
)

func (Store) ReadGetAPIUsageAggregate(q getapi_aggregate.GetAPIUsageAggregateRequest, limit int) ([]getapi_aggregate.GetAPIUsageAggregateRow, error) {
	selection := []string{"user_id", "SUM(quota) AS quota", "SUM(count) AS count", "SUM(token_used) AS token_used"}
	grouping := []string{"user_id"}
	var args []any
	if q.GroupBy == "time" || q.GroupBy == "time_model" {
		bucket, bucketArgs, err := quotaBucketExpression(*q.Granularity, *q.StartTimestamp, *q.EndTimestamp)
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
	if q.GroupBy == "token" {
		selection = append(selection, "token_id")
		grouping = append(grouping, "token_id")
	}
	query := model.DB.Table("quota_data").Select(strings.Join(selection, ", "), args...).
		Where("user_id IN ? AND created_at >= ? AND created_at <= ?", q.UserIDs, *q.StartTimestamp, *q.EndTimestamp)
	if q.TokenID != nil {
		query = query.Where("token_id = ?", *q.TokenID)
	}
	rows := make([]getapi_aggregate.GetAPIUsageAggregateRow, 0)
	if err := query.Group(strings.Join(grouping, ", ")).Order(strings.Join(grouping, ", ")).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	if q.GroupBy != "token" || len(rows) >= limit {
		return rows, nil
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.TokenID != nil && *row.TokenID > 0 {
			ids = append(ids, *row.TokenID)
		}
	}
	tokens := make([]model.Token, 0)
	if len(ids) > 0 {
		if err := model.DB.Unscoped().Select("id", "name", "deleted_at").Where("id IN ?", ids).Find(&tokens).Error; err != nil {
			return nil, err
		}
	}
	names := make(map[int]model.Token, len(tokens))
	for _, token := range tokens {
		names[token.Id] = token
	}
	for i := range rows {
		deleted := false
		if rows[i].TokenID != nil {
			if token, ok := names[*rows[i].TokenID]; ok {
				rows[i].TokenName = &token.Name
				deleted = token.DeletedAt.Valid
			}
		}
		rows[i].TokenDeleted = &deleted
	}
	return rows, nil
}

func (Store) ReadGetAPIUsageRecent(ids []int, tokenID *int, since int64) ([]getapi_aggregate.GetAPIUsageRecent, error) {
	rows := make([]getapi_aggregate.GetAPIUsageRecent, 0)
	query := model.LOG_DB.Table("logs").Select("user_id, COUNT(*) AS rpm, COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS tpm").
		Where("user_id IN ? AND type = ? AND created_at >= ?", ids, domain.LogTypeConsume, since)
	if tokenID != nil {
		query = query.Where("token_id = ?", *tokenID)
	}
	err := query.Group("user_id").Scan(&rows).Error
	return rows, err
}
