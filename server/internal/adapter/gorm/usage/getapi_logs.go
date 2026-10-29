package usage

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/getapi_logs"
	"github.com/QuantumNous/new-api/model"
)

func (Store) ReadGetAPIUsageLogs(q getapi_logs.GetAPIUsageLogsRequest, policy domain.VisibilityPolicy) (getapi_logs.GetAPIUsageLogsResult, error) {
	result := getapi_logs.GetAPIUsageLogsResult{}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return result, domain.ErrGetAPIUsageUnsupported
	}
	query := model.LOG_DB.Model(&record.Log{}).
		Where("user_id = ? AND created_at >= ? AND created_at <= ? AND type IN ?", q.UserID, *q.StartTimestamp, *q.EndTimestamp, q.Types)
	if q.TokenID != nil {
		query = query.Where("token_id = ?", *q.TokenID)
	}
	if q.ModelName != nil {
		query = query.Where("model_name = ?", *q.ModelName)
	}
	if q.WithTotal {
		var total int64
		if err := query.Count(&total).Error; err != nil {
			return result, err
		}
		result.Total = &total
	}
	order := "id DESC"
	if q.BeforeID != nil {
		query = query.Where("id < ?", *q.BeforeID)
	}
	if q.AfterID != nil {
		query = query.Where("id > ?", *q.AfterID)
		order = "id ASC"
	}
	rows := make([]*record.Log, 0)
	if err := query.Order(order).Limit(q.Limit + 1).Find(&rows).Error; err != nil {
		return result, err
	}
	result.HasMore = len(rows) > q.Limit
	if result.HasMore {
		rows = rows[:q.Limit]
	}
	if q.AfterID != nil {
		slices.Reverse(rows)
	}
	projectLogs(rows, policy, 0)
	result.Items = make([]getapi_logs.GetAPIUsageLog, len(rows))
	for i, row := range rows {
		result.Items[i] = getapi_logs.GetAPIUsageLog{
			ID: row.Id, CreatedAt: row.CreatedAt, Type: row.Type, Content: row.Content,
			Username: row.Username, TokenName: row.TokenName, TokenID: row.TokenId,
			ModelName: row.ModelName, Quota: row.Quota, PromptTokens: row.PromptTokens,
			CompletionTokens: row.CompletionTokens, UseTime: row.UseTime, IsStream: row.IsStream,
			Group: row.Group, RequestID: row.RequestId, UpstreamRequestID: row.UpstreamRequestId,
			Other: row.Other, UserID: row.UserId,
		}
	}
	return result, nil
}
