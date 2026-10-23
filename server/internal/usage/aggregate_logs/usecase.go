package aggregate_logs

import "github.com/QuantumNous/new-api/internal/usage/domain"

type Reader interface {
	CountAggregateUsers([]int) (int64, error)
	ReadLogAggregate(domain.AggregateScope, int) (domain.LogAggregate, error)
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

func (u Usecase) Execute(q domain.AggregateScope) (domain.LogAggregate, error) {
	if err := q.Validate(); err != nil {
		return domain.LogAggregate{}, err
	}
	count, err := u.reader.CountAggregateUsers(q.UserIDs)
	if err != nil {
		return domain.LogAggregate{}, err
	}
	if count != int64(len(q.UserIDs)) {
		return domain.LogAggregate{}, domain.ErrAggregateUserNotFound
	}
	result, err := u.reader.ReadLogAggregate(q, domain.LogTypeConsume)
	if err != nil {
		return domain.LogAggregate{}, err
	}
	if err := domain.ValidateAggregateIntegers(result.Quota, result.RequestCount); err != nil {
		return domain.LogAggregate{}, err
	}
	return result, nil
}
