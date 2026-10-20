package begin_passkey_registration

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	LoadCredential() error
	CredentialMissing(error) bool
	ClearCredential()
	Build() error
	BeginRegistration() error
	ReadIdentity() bool
	CreateRegistrationFlow() error
}

func (usecase Usecase) Execute() (domain.PasskeyFailure, error) {
	err := usecase.operation.LoadCredential()
	if err != nil && !usecase.operation.CredentialMissing(err) {
		return "", err
	}
	if usecase.operation.CredentialMissing(err) {
		usecase.operation.ClearCredential()
	}
	if err := usecase.operation.Build(); err != nil {
		return "", err
	}
	if err := usecase.operation.BeginRegistration(); err != nil {
		return "", err
	}
	if !usecase.operation.ReadIdentity() {
		return domain.PasskeyIdentityRequired, nil
	}
	return "", usecase.operation.CreateRegistrationFlow()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
