package finish_passkey_login

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	Build() error
	PopLoginFlow() error
	VerificationRequired() bool
	ValidateDiscoverable() error
	LoginAccountValid() bool
	LoginAccountEnabled() bool
	UpdateAssertion() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.Build(); err != nil {
		return err
	}
	if err := usecase.operation.PopLoginFlow(); err != nil {
		return err
	}
	if !usecase.operation.VerificationRequired() {
		return domain.ErrAuthFlowInvalid
	}
	if err := usecase.operation.ValidateDiscoverable(); err != nil {
		return err
	}
	if !usecase.operation.LoginAccountValid() {
		return domain.ErrPasskeyLoginInvalid
	}
	if !usecase.operation.LoginAccountEnabled() {
		return domain.ErrPasskeyAccountDisabled
	}
	return usecase.operation.UpdateAssertion()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
