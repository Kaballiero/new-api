package admin_query_user_logs

import (
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type Reader interface {
	QueryUserLogs(domain.LogQuery, domain.VisibilityPolicy) ([]*dto.Log, int64, error)
	ValidateModelFilter(string) error
}

type Input struct {
	Query    domain.LogQuery
	Audience domain.Audience
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
	if err := in.Query.ValidateStrict(); err != nil {
		return Result{}, err
	}
	if err := u.reader.ValidateModelFilter(in.Query.ModelName); err != nil {
		return Result{}, domain.ErrInvalidLogQuery
	}
	ids := make([]int, 0, len(in.Query.UserIDs))
	seen := make(map[int]bool)
	for _, id := range in.Query.UserIDs {
		if id <= 0 || id > domain.MaxLogUserID {
			return Result{}, domain.ErrInvalidLogQuery
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	in.Query.UserIDs = ids
	if len(ids) == 0 {
		return Result{Items: make([]*dto.Log, 0)}, nil
	}
	rows, total, err := u.reader.QueryUserLogs(in.Query, domain.LogVisibility(in.Audience))
	return Result{Items: rows, Total: total}, err
}
