package admin_get_usage_flow

import (
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type Reader interface {
	ReadFlow(domain.FlowQuery) ([]*dto.FlowQuotaData, error)
	ResolveFlowTokens([]*dto.FlowQuotaData) error
	ResolveFlowChannels([]*dto.FlowQuotaData) error
}

type Input struct {
	Query    domain.FlowQuery
	Audience domain.Audience
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

func (u Usecase) Execute(in Input) ([]*dto.FlowQuotaData, error) {
	q := in.Query
	q.Projection = domain.FlowAdmin
	if in.Audience == domain.AudienceRoot {
		q.Projection = domain.FlowRoot
	}
	rows, err := u.reader.ReadFlow(q)
	if err != nil {
		return nil, err
	}
	if q.Projection == domain.FlowRoot {
		if err := u.reader.ResolveFlowTokens(rows); err != nil {
			return rows, err
		}
	}
	if err := u.reader.ResolveFlowChannels(rows); err != nil {
		return rows, err
	}
	for _, row := range rows {
		row.ChannelName = domain.ChannelDisplayName(row.ChannelID, row.ChannelName)
	}
	return rows, nil
}
