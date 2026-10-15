package model

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
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

var quotaBucketDimensions = map[QuotaBucketGrouping]struct{ columns, order string }{
	QuotaBucketByModel:        {"model_name", "bucket_start, model_name"},
	QuotaBucketByUserAndModel: {"user_id, username, model_name", "bucket_start, model_name, user_id, username"},
	QuotaBucketByUser:         {"username", "bucket_start, username"},
}

type BucketedQuotaData struct {
	UserID    int    `json:"user_id" gorm:"column:user_id"`
	Username  string `json:"username" gorm:"column:username"`
	ModelName string `json:"model_name" gorm:"column:model_name"`
	CreatedAt int64  `json:"created_at" gorm:"column:bucket_start"`
	TokenUsed int64  `json:"token_used" gorm:"column:token_used"`
	Count     int64  `json:"count" gorm:"column:count"`
	Quota     int64  `json:"quota" gorm:"column:quota"`
}

type BucketedUserQuotaData struct {
	Username  string `json:"username" gorm:"column:username"`
	CreatedAt int64  `json:"created_at" gorm:"column:bucket_start"`
	TokenUsed int64  `json:"token_used" gorm:"column:token_used"`
	Count     int64  `json:"count" gorm:"column:count"`
	Quota     int64  `json:"quota" gorm:"column:quota"`
}

type BucketedQuotaQuery struct {
	StartTime   int64
	EndTime     int64
	Username    string
	UserID      int
	TokenID     int
	Granularity string
	Grouping    QuotaBucketGrouping
}

func IsValidQuotaGranularity(granularity string) bool {
	switch granularity {
	case GranularityHour, GranularityDay, GranularityWeek, GranularityMonth:
		return true
	}
	return false
}

func GetBucketedQuotaDates(q BucketedQuotaQuery) ([]*BucketedQuotaData, error) {
	query, err := bucketedQuotaQuery(q)
	if err != nil {
		return nil, err
	}
	rows := make([]*BucketedQuotaData, 0)
	err = query.Find(&rows).Error
	return rows, err
}

func GetBucketedQuotaDatesByUser(q BucketedQuotaQuery) ([]*BucketedUserQuotaData, error) {
	query, err := bucketedQuotaQuery(q)
	if err != nil {
		return nil, err
	}
	rows := make([]*BucketedUserQuotaData, 0)
	err = query.Find(&rows).Error
	return rows, err
}

func bucketedQuotaQuery(q BucketedQuotaQuery) (*gorm.DB, error) {
	dimensions, ok := quotaBucketDimensions[q.Grouping]
	if !ok {
		return nil, ErrInvalidQuotaGrouping
	}
	bucket, args, err := quotaBucketExpression(q.Granularity, q.StartTime, q.EndTime)
	if err != nil {
		return nil, err
	}
	selection := dimensions.columns + ", " + bucket + " as bucket_start, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used"
	query := DB.Table("quota_data").
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
	case GranularityHour:
		return "created_at - (created_at % 3600)", nil, nil
	case GranularityDay:
		return "created_at - (created_at % 86400)", nil, nil
	case GranularityWeek:
		return "created_at - (((created_at % 604800) + 259200) % 604800)", nil, nil
	case GranularityMonth:
		boundaries := quotaMonthBoundaries(startTime, endTime)
		if len(boundaries)-1 > MaxBucketedMonths {
			return "", nil, ErrQuotaMonthSpanExceeded
		}
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
	return "", nil, ErrInvalidQuotaGranularity
}

func quotaMonthBoundaries(startTime int64, endTime int64) []int64 {
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
