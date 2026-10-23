package domain

import (
	"errors"
	"fmt"
)

var (
	ErrUsageInvalidStart         = errors.New("invalid start_timestamp")
	ErrUsageInvalidEnd           = errors.New("invalid end_timestamp")
	ErrUsageInvalidRange         = errors.New("invalid time range")
	ErrUsageInvalidTokenID       = errors.New("token_id must be a positive integer")
	ErrUsageAggregateRequestSize = errors.New("aggregate request exceeds 16 KiB")
	ErrUsageAggregateJSON        = errors.New("invalid aggregate JSON body")
	ErrUsageAggregateUserCount   = fmt.Errorf("%w: user_ids must contain 1..100 positive integers", ErrInvalidAggregate)
	ErrUsageAggregateUserIDs     = fmt.Errorf("%w: user_ids must be positive safe integers", ErrInvalidAggregate)
	ErrUsageAggregateTimes       = fmt.Errorf("%w: inclusive timestamps must be nonnegative safe integers in order", ErrInvalidAggregate)
	ErrUsageAggregateGranularity = fmt.Errorf("%w: %s", ErrInvalidAggregate, ErrInvalidQuotaGranularity)
	ErrUsageAggregateMonths      = fmt.Errorf("%w: %s", ErrInvalidAggregate, ErrQuotaMonthSpanExceeded)
	ErrUsageAggregateGroup       = fmt.Errorf("%w: group_by must be time, model, time_model or user", ErrInvalidAggregate)
)

// InvalidRankingPeriod identifies an unsupported period without parsing its message.
type InvalidRankingPeriod struct {
	Period string
}

func (e *InvalidRankingPeriod) Error() string {
	return fmt.Sprintf("invalid ranking period: %s", e.Period)
}
