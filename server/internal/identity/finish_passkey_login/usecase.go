package finish_passkey_login

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	Build() error
	PopLoginFlow() error
	VerificationRequired() bool
	InvalidFlow() error
	ValidateDiscoverable() error
	LoginAccountValid() bool
	LoginAccountEnabled() bool
	UpdateAssertion() error
}

func (usecase Usecase) Execute() (domain.PasskeyFailure, error) {
	if err := usecase.operation.Build(); err != nil {
		return "", err
	}
	if err := usecase.operation.PopLoginFlow(); err != nil {
		return "", err
	}
	if !usecase.operation.VerificationRequired() {
		return "", usecase.operation.InvalidFlow()
	}
	if err := usecase.operation.ValidateDiscoverable(); err != nil {
		return "", err
	}
	if !usecase.operation.LoginAccountValid() {
		return domain.PasskeyLoginInvalid, nil
	}
	if !usecase.operation.LoginAccountEnabled() {
		return domain.PasskeyAccountDisabled, nil
	}
	return "", usecase.operation.UpdateAssertion()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
