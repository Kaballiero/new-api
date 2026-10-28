package validate_authorization

import "github.com/QuantumNous/new-api/internal/identity/domain"

type FlowAuthorizationOperation interface {
	AuthorizationValid() bool
	BindOperation() error
	BindingMatches() bool
	ValidateSession() error
	RequireAuthorizedMethod() error
}

func (usecase Usecase) Execute(operation FlowAuthorizationOperation) error {
	if !operation.AuthorizationValid() {
		return domain.ErrAuthFlowInvalid
	}
	if err := operation.BindOperation(); err != nil {
		return err
	}
	if !operation.BindingMatches() {
		return domain.ErrAuthFlowInvalid
	}
	if err := operation.ValidateSession(); err != nil {
		return err
	}
	return operation.RequireAuthorizedMethod()
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
