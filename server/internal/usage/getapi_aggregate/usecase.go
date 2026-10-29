package getapi_aggregate

import "github.com/QuantumNous/new-api/internal/usage/domain"

type Reader interface {
	ReadGetAPIUsageAggregate(GetAPIUsageAggregateRequest, int) ([]GetAPIUsageAggregateRow, error)
	ReadGetAPIUsageRecent([]int, *int, int64) ([]GetAPIUsageRecent, error)
}

type Usecase struct {
	reader Reader
	now    func() int64
}

func NewUsecase(reader Reader, now func() int64) Usecase {
	return Usecase{reader: reader, now: now}
}

func (u Usecase) Execute(q GetAPIUsageAggregateRequest) (GetAPIUsageAggregateResult, error) {
	if err := domain.ValidateGetAPIUsageWindow(q.StartTimestamp, q.EndTimestamp); err != nil {
		return GetAPIUsageAggregateResult{}, err
	}
	if len(q.UserIDs) == 0 || len(q.UserIDs) > domain.MaxAggregateUsers || q.TokenID != nil && *q.TokenID <= 0 {
		return GetAPIUsageAggregateResult{}, domain.ErrGetAPIUsageInvalidQuery
	}
	seen := make(map[int]bool, len(q.UserIDs))
	for _, id := range q.UserIDs {
		if id <= 0 || seen[id] {
			return GetAPIUsageAggregateResult{}, domain.ErrGetAPIUsageInvalidQuery
		}
		seen[id] = true
	}
	switch q.GroupBy {
	case "model", "token":
		if q.Granularity != nil {
			return GetAPIUsageAggregateResult{}, domain.ErrGetAPIUsageInvalidQuery
		}
	case "time", "time_model":
		if q.Granularity == nil || !domain.IsValidQuotaGranularity(*q.Granularity) || *q.Granularity == "hour" && *q.EndTimestamp-*q.StartTimestamp > 7*86400 {
			return GetAPIUsageAggregateResult{}, domain.ErrGetAPIUsageInvalidQuery
		}
	default:
		return GetAPIUsageAggregateResult{}, domain.ErrGetAPIUsageInvalidQuery
	}
	rows, err := u.reader.ReadGetAPIUsageAggregate(q, domain.MaxAggregateRows+1)
	if err != nil {
		return GetAPIUsageAggregateResult{}, err
	}
	if len(rows) > domain.MaxAggregateRows {
		return GetAPIUsageAggregateResult{}, domain.ErrAggregateTooLarge
	}
	result := GetAPIUsageAggregateResult{Rows: rows}
	if q.GroupBy == "model" {
		recent, err := u.reader.ReadGetAPIUsageRecent(q.UserIDs, q.TokenID, u.now()-60)
		if err != nil {
			return GetAPIUsageAggregateResult{}, err
		}
		rates := make(map[int]GetAPIUsageRecent, len(recent))
		for _, row := range recent {
			rates[row.UserID] = row
		}
		result.Recent = make([]GetAPIUsageRecent, len(q.UserIDs))
		for i, id := range q.UserIDs {
			result.Recent[i] = rates[id]
			result.Recent[i].UserID = id
		}
	}
	return result, nil
}
