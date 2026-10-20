package admin_disable_twofa

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Operation interface {
	LoadTarget(id int) (role int, err error)
	MissingError() error
	NotEnabledError() error
	ActorRole() int
	RootRole() int
	Disable(id int) error
	RevokeAll(id int, reason string) error
}

func (usecase Usecase) Execute(id int) (Output, error) {
	role, err := usecase.operation.LoadTarget(id)
	if errors.Is(err, usecase.operation.MissingError()) {
		return Output{Issue: "missing"}, nil
	}
	if err != nil {
		return Output{}, err
	}
	if !domain.CanManageTargetRole(usecase.operation.ActorRole(), role, usecase.operation.RootRole()) {
		return Output{Issue: "forbidden"}, nil
	}
	if err := usecase.operation.Disable(id); err != nil {
		if errors.Is(err, usecase.operation.NotEnabledError()) {
			return Output{Issue: "disabled"}, nil
		}
		return Output{}, err
	}
	return Output{}, usecase.operation.RevokeAll(id, "admin_twofa_disabled")
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
