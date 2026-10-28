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

func (usecase Usecase) Execute(operation ConsumeProofOperation) error {
	if err := operation.BindOperation(); err != nil {
		return err
	}
	method, err := operation.VerifyClaims()
	if err != nil {
		return err
	}
	if err := operation.ValidateSession(); err != nil {
		return err
	}
	if err := operation.RequireMethod(method); err != nil {
		return err
	}
	err = operation.ConsumeProof()
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
	operation.CaptureAuthorization()
	return nil
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
