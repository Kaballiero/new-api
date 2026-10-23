package list_own_events

import (
	"github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/audit/dto"
)

type Reader interface {
	List(domain.Query, domain.VisibilityPolicy) ([]*dto.Event, int64, error)
}

type Input struct {
	Query  domain.Query
	Viewer domain.Actor
}

type Result struct {
	Items []*dto.Event
	Total int64
}

type Usecase struct {
	reader Reader
	roles  domain.Roles
}

func NewUsecase(reader Reader, roles domain.Roles) Usecase {
	return Usecase{reader: reader, roles: roles}
}

func (u Usecase) Execute(in Input) (Result, error) {
	query := in.Query
	query.UserId, query.Username = in.Viewer.ID, ""
	rows, total, err := u.reader.List(query, domain.EventVisibility(in.Viewer.Role, u.roles, true))
	return Result{Items: rows, Total: total}, err
}
