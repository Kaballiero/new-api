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

func (usecase Usecase) Execute(operation Operation) error {
	err := operation.LoadCredential()
	if err != nil && !operation.CredentialMissing(err) {
		return err
	}
	if operation.CredentialMissing(err) {
		operation.ClearCredential()
	}
	if err := operation.Build(); err != nil {
		return err
	}
	if err := operation.BeginRegistration(); err != nil {
		return err
	}
	if !operation.ReadIdentity() {
		return domain.ErrPasskeyIdentityRequired
	}
	return operation.CreateRegistrationFlow()
}

type Usecase struct{}

func NewUsecase() Usecase { return Usecase{} }
