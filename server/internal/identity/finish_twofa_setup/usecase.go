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

func FinishTwoFASetup(op FinishTwoFASetupOperation, e domain.EnrollmentErrors) error {
	err := op.LoadSetupFlow()
	if errors.Is(err, e.FlowInvalid()) || errors.Is(err, e.FlowExpired()) || errors.Is(err, e.FlowConsumed()) {
		return e.SetupInvalid()
	}
	if err != nil {
		return err
	}
	if err := op.DecodeAuthorization(); err != nil {
		return err
	}
	if err := op.ValidateAuthorization(); err != nil {
		if errors.Is(err, e.FlowInvalid()) {
			return e.SetupInvalid()
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
