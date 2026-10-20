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
	case errors.Is(err, usecase.errors.FlowConsumed()):
		return usecase.errors.ProofConsumed()
	case errors.Is(err, usecase.errors.FlowExpired()):
		return usecase.errors.TokenExpired()
	case errors.Is(err, usecase.errors.FlowInvalid()):
		return usecase.errors.TokenInvalid()
	case err != nil:
		return err
	}
	usecase.operation.CaptureAuthorization()
	return nil
}

type Usecase struct {
	operation ConsumeProofOperation
	errors    domain.VerificationErrors
}

func NewUsecase(operation ConsumeProofOperation, errors domain.VerificationErrors) Usecase {
	return Usecase{operation: operation, errors: errors}
}
