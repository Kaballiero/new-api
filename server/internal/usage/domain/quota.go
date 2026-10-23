package domain

import (
	"errors"
	"fmt"
)

const MaxOwnQuotaSpanSeconds = 30 * 24 * 60 * 60

var ErrQuotaSpan = errors.New("time span exceeds 1 month")

var ErrPositiveQuotaTimes = errors.New("start_timestamp and end_timestamp must be positive")

type QuotaMode uint8

const (
	QuotaModels QuotaMode = iota
	QuotaUsername
	QuotaOwner
	QuotaUsers
)

type QuotaQuery struct {
	StartTime, EndTime int64
	Username           string
	UserID, TokenID    int
	Granularity        string
	Grouping           QuotaBucketGrouping
	Mode               QuotaMode
}

func ValidateQuotaGranularity(value string) error {
	if value != "" && !IsValidQuotaGranularity(value) {
		return ErrInvalidQuotaGranularity
	}
	return nil
}

func ValidateQuotaBucket(q QuotaQuery) error {
	switch q.Grouping {
	case QuotaBucketByModel, QuotaBucketByUserAndModel, QuotaBucketByUser:
	default:
		return ErrInvalidQuotaGrouping
	}
	if !IsValidQuotaGranularity(q.Granularity) {
		return ErrInvalidQuotaGranularity
	}
	if q.Granularity == GranularityMonth && len(QuotaMonthBoundaries(q.StartTime, q.EndTime))-1 > MaxBucketedMonths {
		return ErrQuotaMonthSpanExceeded
	}
	return nil
}

type FlowProjection uint8

const (
	FlowSelf FlowProjection = iota
	FlowAdmin
	FlowRoot
)

type FlowQuery struct {
	StartTime, EndTime int64
	Username           string
	UserID             int
	Projection         FlowProjection
}

func ChannelDisplayName(id int, name string) string {
	if name == "" && id > 0 {
		return fmt.Sprintf("channel-%d", id)
	}
	return name
}
