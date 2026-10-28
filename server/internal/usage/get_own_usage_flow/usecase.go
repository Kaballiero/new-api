package get_own_usage_flow

import (
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type Reader interface {
	ReadFlow(domain.FlowQuery) ([]*dto.FlowQuotaData, error)
	ResolveFlowTokens([]*dto.FlowQuotaData) error
}

type Input struct {
	Query domain.FlowQuery
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

func (u Usecase) Execute(in Input) ([]*dto.FlowQuotaData, error) {
	q := in.Query
	if q.EndTime-q.StartTime > domain.MaxOwnQuotaSpanSeconds {
		return nil, domain.ErrQuotaSpan
	}
	q.Projection = domain.FlowSelf
	q.Username = ""
	rows, err := u.reader.ReadFlow(q)
	if err != nil {
		return nil, err
	}
	if err := u.reader.ResolveFlowTokens(rows); err != nil {
		return rows, err
	}
	return rows, nil
}
