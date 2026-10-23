package admin_list_logs

import (
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type Reader interface {
	ListAdminLogs(domain.LogQuery, domain.VisibilityPolicy) ([]*dto.Log, int64, error)
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
	rows, total, err := u.reader.ListAdminLogs(in.Query, domain.LogVisibility(in.Audience))
	return Result{Items: rows, Total: total}, err
}
