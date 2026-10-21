package finish_twofa_setup

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type FinishTwoFASetupOperation interface {
	LoadSetupFlow() error
	DecodeAuthorization() error
	ValidateAuthorization() error
	EnableEnrollment() error
}

func FinishTwoFASetup(op FinishTwoFASetupOperation) error {
	err := op.LoadSetupFlow()
	if errors.Is(err, domain.ErrAuthFlowInvalid) || errors.Is(err, domain.ErrAuthFlowExpired) || errors.Is(err, domain.ErrAuthFlowConsumed) {
		return domain.ErrTwoFASetupInvalid
	}
	if err != nil {
		return err
	}
	if err := op.DecodeAuthorization(); err != nil {
		return err
	}
	if err := op.ValidateAuthorization(); err != nil {
		if errors.Is(err, domain.ErrAuthFlowInvalid) {
			return domain.ErrTwoFASetupInvalid
		}
		return err
	}
	return op.EnableEnrollment()
}

type EnableOperation interface {
	FinishEnrollment() error
	AdvanceSession() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.FinishEnrollment(); err != nil {
		return err
	}
	return usecase.operation.AdvanceSession()
}

type Usecase struct {
	operation EnableOperation
}

func NewUsecase(operation EnableOperation) Usecase {
	return Usecase{operation: operation}
}
