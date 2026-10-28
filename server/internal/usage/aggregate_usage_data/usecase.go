package aggregate_usage_data

import "github.com/QuantumNous/new-api/internal/usage/domain"

type Reader interface {
	CountAggregateUsers([]int) (int64, error)
	ReadUsageAggregate(domain.UsageAggregateQuery, int) ([]domain.UsageAggregateRow, error)
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

func (u Usecase) Execute(q domain.UsageAggregateQuery) ([]domain.UsageAggregateRow, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	count, err := u.reader.CountAggregateUsers(q.UserIDs)
	if err != nil {
		return nil, err
	}
	if count != int64(len(q.UserIDs)) {
		return nil, domain.ErrAggregateUserNotFound
	}
	rows, err := u.reader.ReadUsageAggregate(q, domain.MaxAggregateRows+1)
	if err != nil {
		return nil, err
	}
	if len(rows) > domain.MaxAggregateRows {
		return nil, domain.ErrAggregateTooLarge
	}
	for _, row := range rows {
		if err := domain.ValidateAggregateIntegers(row.Quota, row.Count, row.TokenUsed); err != nil {
			return nil, err
		}
	}
	return rows, nil
}
