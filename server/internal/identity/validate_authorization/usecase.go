package validate_authorization

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type FlowAuthorizationOperation interface {
	AuthorizationValid() bool
	BindOperation() error
	BindingMatches() bool
	ValidateSession() error
	RequireAuthorizedMethod() error
}

func (usecase Usecase) Execute() error {
	if !usecase.operation.AuthorizationValid() {
		return domain.ErrAuthFlowInvalid
	}
	if err := usecase.operation.BindOperation(); err != nil {
		return err
	}
	if !usecase.operation.BindingMatches() {
		return domain.ErrAuthFlowInvalid
	}
	if err := usecase.operation.ValidateSession(); err != nil {
		return err
	}
	return usecase.operation.RequireAuthorizedMethod()
}

type Usecase struct {
	operation FlowAuthorizationOperation
}

func NewUsecase(operation FlowAuthorizationOperation) Usecase {
	return Usecase{operation: operation}
}
