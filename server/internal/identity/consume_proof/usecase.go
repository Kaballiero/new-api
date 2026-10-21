package consume_proof

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type ConsumeProofOperation interface {
	BindOperation() error
	VerifyClaims() (method string, err error)
	ValidateSession() error
	RequireMethod(method string) error
	ConsumeProof() error
	CaptureAuthorization()
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.BindOperation(); err != nil {
		return err
	}
	method, err := usecase.operation.VerifyClaims()
	if err != nil {
		return err
	}
	if err := usecase.operation.ValidateSession(); err != nil {
		return err
	}
	if err := usecase.operation.RequireMethod(method); err != nil {
		return err
	}
	err = usecase.operation.ConsumeProof()
	switch {
	case errors.Is(err, domain.ErrAuthFlowConsumed):
		return domain.ErrProofConsumed
	case errors.Is(err, domain.ErrAuthFlowExpired):
		return domain.ErrAuthTokenExpired
	case errors.Is(err, domain.ErrAuthFlowInvalid):
		return domain.ErrAuthTokenInvalid
	case err != nil:
		return err
	}
	usecase.operation.CaptureAuthorization()
	return nil
}

type Usecase struct {
	operation ConsumeProofOperation
}

func NewUsecase(operation ConsumeProofOperation) Usecase {
	return Usecase{operation: operation}
}
