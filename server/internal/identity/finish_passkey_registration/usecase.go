package finish_passkey_registration

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	Build() error
	LoadCredential() error
	CredentialMissing(error) bool
	ClearCredential()
	ReadIdentity() bool
	PopRegistrationFlow() error
	VerificationRequired() bool
	ValidateEnrollmentAuthorization() error
	CreateCredential() error
	CredentialRecordValid() bool
	RegisterCredential() error
	AdvanceSession() error
}

func (usecase Usecase) Execute(operation Operation) error {
	if err := operation.Build(); err != nil {
		return err
	}
	err := operation.LoadCredential()
	if err != nil && !operation.CredentialMissing(err) {
		return err
	}
	if operation.CredentialMissing(err) {
		operation.ClearCredential()
	}
	if !operation.ReadIdentity() {
		return domain.ErrPasskeyIdentityRequired
	}
	if err := operation.PopRegistrationFlow(); err != nil {
		return err
	}
	if !operation.VerificationRequired() {
		return domain.ErrAuthFlowInvalid
	}
	if err := operation.ValidateEnrollmentAuthorization(); err != nil {
		return err
	}
	if err := operation.CreateCredential(); err != nil {
		return err
	}
	if !operation.CredentialRecordValid() {
		return domain.ErrPasskeyRecordInvalid
	}
	if err := operation.RegisterCredential(); err != nil {
		return err
	}
	return operation.AdvanceSession()
}

type Usecase struct{}

func NewUsecase() Usecase { return Usecase{} }
