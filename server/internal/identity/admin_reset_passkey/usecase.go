package admin_reset_passkey

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Operation interface {
	LoadTarget(id int) (userID int, username string, role int, err error)
	MissingError() error
	ActorRole() int
	RootRole() int
	RequireCredential(id int) error
	DeleteCredential(id int) error
	RevokeAll(id int, reason string) error
}

func (usecase Usecase) Execute(id int) (Output, error) {
	userID, username, role, err := usecase.operation.LoadTarget(id)
	if err != nil {
		return Output{}, err
	}
	if !domain.CanManageTargetRole(usecase.operation.ActorRole(), role, usecase.operation.RootRole()) {
		return Output{Issue: "forbidden"}, nil
	}
	if err := usecase.operation.RequireCredential(userID); err != nil {
		if errors.Is(err, usecase.operation.MissingError()) {
			return Output{Issue: "missing"}, nil
		}
		return Output{}, err
	}
	if err := usecase.operation.DeleteCredential(userID); err != nil {
		return Output{}, err
	}
	if err := usecase.operation.RevokeAll(userID, "admin_passkey_reset"); err != nil {
		return Output{}, err
	}
	return Output{ID: userID, Username: username}, nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
