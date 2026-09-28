package model

import (
	"errors"
	"strconv"
	"strings"
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
)

type BucketedQuotaData struct {
	UserID    int    `json:"user_id" gorm:"column:user_id"`
	Username  string `json:"username" gorm:"column:username"`
	ModelName string `json:"model_name" gorm:"column:model_name"`
	CreatedAt int64  `json:"created_at" gorm:"column:bucket_start"`
	TokenUsed int    `json:"token_used" gorm:"column:token_used"`
	Count     int    `json:"count" gorm:"column:count"`
	Quota     int    `json:"quota" gorm:"column:quota"`
}

type BucketedQuotaQuery struct {
	StartTime   int64
	EndTime     int64
	Username    string
	UserID      int
	TokenID     int
	Granularity string
}

func IsValidQuotaGranularity(granularity string) bool {
	switch granularity {
	case GranularityHour, GranularityDay, GranularityWeek, GranularityMonth:
		return true
	}
	return false
}

func GetBucketedQuotaDates(q BucketedQuotaQuery) ([]*BucketedQuotaData, error) {
	bucket, err := quotaBucketExpression(q.Granularity, q.StartTime, q.EndTime)
	if err != nil {
		return nil, err
	}
	dimensions := "model_name"
	if q.Username != "" || q.UserID > 0 {
		dimensions = "user_id, username, model_name"
	}
	selection := dimensions + ", " + bucket + " as bucket_start, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used"
	query := DB.Table("quota_data").
		Select(selection).
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
	rows := make([]*BucketedQuotaData, 0)
	err = query.
		Group(dimensions + ", bucket_start").
		Order("bucket_start, model_name").
		Find(&rows).Error
	return rows, err
}

func quotaBucketExpression(granularity string, startTime int64, endTime int64) (string, error) {
	switch granularity {
	case GranularityHour:
		return "created_at - (created_at % 3600)", nil
	case GranularityDay:
		return "created_at - (created_at % 86400)", nil
	case GranularityWeek:
		return "created_at - ((created_at + 259200) % 604800)", nil
	case GranularityMonth:
		boundaries := quotaMonthBoundaries(startTime, endTime)
		if len(boundaries)-1 > MaxBucketedMonths {
			return "", ErrQuotaMonthSpanExceeded
		}
		expression := strings.Builder{}
		expression.WriteString("CASE")
		for i := 1; i < len(boundaries); i++ {
			expression.WriteString(" WHEN created_at < " + strconv.FormatInt(boundaries[i], 10) +
				" THEN " + strconv.FormatInt(boundaries[i-1], 10))
		}
		expression.WriteString(" ELSE " + strconv.FormatInt(boundaries[len(boundaries)-1], 10) + " END")
		return expression.String(), nil
	}
	return "", ErrInvalidQuotaGranularity
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
