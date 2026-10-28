package usage

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var quotaBucketDimensions = map[domain.QuotaBucketGrouping]struct{ columns, order string }{
	domain.QuotaBucketByModel:        {"model_name", "bucket_start, model_name"},
	domain.QuotaBucketByUserAndModel: {"user_id, username, model_name", "bucket_start, model_name, user_id, username"},
	domain.QuotaBucketByUser:         {"username", "bucket_start, username"},
}

type bucketedQuotaRow struct {
	UserID    int    `gorm:"column:user_id"`
	Username  string `gorm:"column:username"`
	ModelName string `gorm:"column:model_name"`
	CreatedAt int64  `gorm:"column:bucket_start"`
	TokenUsed int64  `gorm:"column:token_used"`
	Count     int64  `gorm:"column:count"`
	Quota     int64  `gorm:"column:quota"`
}

type bucketedUserQuotaRow struct {
	Username  string `gorm:"column:username"`
	CreatedAt int64  `gorm:"column:bucket_start"`
	TokenUsed int64  `gorm:"column:token_used"`
	Count     int64  `gorm:"column:count"`
	Quota     int64  `gorm:"column:quota"`
}

func bucketedQuotaQuery(q domain.QuotaQuery) (*gorm.DB, error) {
	dimensions := quotaBucketDimensions[q.Grouping]
	bucket, args, err := quotaBucketExpression(q.Granularity, q.StartTime, q.EndTime)
	if err != nil {
		return nil, err
	}
	selection := dimensions.columns + ", " + bucket + " as bucket_start, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used"
	query := model.DB.Table("quota_data").
		Select(selection, args...).
		Where("created_at >= ? and created_at <= ?", q.StartTime, q.EndTime)
	if q.Username != "" {
		query = query.Where("username = ?", q.Username)
	}
	if q.UserID > 0 {
		query = query.Where("user_id = ?", q.UserID)
	}
	if q.TokenID > 0 {
		query = query.Where("token_id = ?", q.TokenID)
	}
	return query.Group(dimensions.columns + ", bucket_start").Order(dimensions.order), nil
}

func quotaBucketExpression(granularity string, startTime int64, endTime int64) (string, []any, error) {
	switch granularity {
	case domain.GranularityHour:
		return "created_at - (created_at % 3600)", nil, nil
	case domain.GranularityDay:
		return "created_at - (created_at % 86400)", nil, nil
	case domain.GranularityWeek:
		return "created_at - (((created_at % 604800) + 259200) % 604800)", nil, nil
	case domain.GranularityMonth:
		boundaries := domain.QuotaMonthBoundaries(startTime, endTime)
		expression := strings.Builder{}
		args := make([]any, 0, 2*len(boundaries)-1)
		expression.WriteString("CAST(CASE")
		for i := 1; i < len(boundaries); i++ {
			expression.WriteString(" WHEN created_at < ? THEN ?")
			args = append(args, boundaries[i], boundaries[i-1])
		}
		expression.WriteString(" ELSE ? END AS DECIMAL(20,0))")
		args = append(args, boundaries[len(boundaries)-1])
		return expression.String(), args, nil
	}
	return "", nil, domain.ErrInvalidQuotaGranularity
}

func (Store) BucketQuota(q domain.QuotaQuery) ([]*dto.BucketedQuotaData, error) {
	query, err := bucketedQuotaQuery(q)
	if err != nil {
		return nil, err
	}
	rows := make([]*bucketedQuotaRow, 0)
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*dto.BucketedQuotaData, len(rows))
	for i, row := range rows {
		value := dto.BucketedQuotaData(*row)
		out[i] = &value
	}
	return out, nil
}

func (Store) BucketUsers(q domain.QuotaQuery) ([]*dto.BucketedUserQuotaData, error) {
	query, err := bucketedQuotaQuery(q)
	if err != nil {
		return nil, err
	}
	rows := make([]*bucketedUserQuotaRow, 0)
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*dto.BucketedUserQuotaData, len(rows))
	for i, row := range rows {
		value := dto.BucketedUserQuotaData(*row)
		out[i] = &value
	}
	return out, nil
}
