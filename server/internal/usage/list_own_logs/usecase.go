package list_own_logs

import (
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type Reader interface {
	ListOwnLogs(domain.LogQuery, domain.VisibilityPolicy) ([]*dto.Log, int64, error)
	TokenOwner(int, int) error
}

type Input struct {
	Query      domain.LogQuery
	TypesQuery []string
}

type Result struct {
	Items []*dto.Log
	Total int64
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

func (u Usecase) Execute(in Input) (Result, error) {
	if in.Query.TokenID > 0 {
		if err := u.reader.TokenOwner(in.Query.TokenID, in.Query.UserID); err != nil {
			return Result{}, err
		}
	}
	if in.TypesQuery != nil {
		var valid bool
		in.Query.Kinds, valid = domain.ParseLogTypesQuery(in.TypesQuery)
		if !valid {
			return Result{}, domain.ErrInvalidLogQuery
		}
	}
	in.Query.CountLimit = domain.MaxLogSearchCount
	in.Query.LiteralModel = in.Query.TokenID > 0 && in.Query.ModelName != ""
	rows, total, err := u.reader.ListOwnLogs(in.Query, domain.LogVisibility(domain.AudienceUser))
	return Result{Items: rows, Total: total}, err
}
