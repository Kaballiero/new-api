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
	InvalidFlow() error
	ValidateEnrollmentAuthorization() error
	CreateCredential() error
	CredentialRecordValid() bool
	RegisterCredential() error
	AdvanceSession() error
}

func (usecase Usecase) Execute() (domain.PasskeyFailure, error) {
	if err := usecase.operation.Build(); err != nil {
		return "", err
	}
	err := usecase.operation.LoadCredential()
	if err != nil && !usecase.operation.CredentialMissing(err) {
		return "", err
	}
	if usecase.operation.CredentialMissing(err) {
		usecase.operation.ClearCredential()
	}
	if !usecase.operation.ReadIdentity() {
		return domain.PasskeyIdentityRequired, nil
	}
	if err := usecase.operation.PopRegistrationFlow(); err != nil {
		return "", err
	}
	if !usecase.operation.VerificationRequired() {
		return "", usecase.operation.InvalidFlow()
	}
	if err := usecase.operation.ValidateEnrollmentAuthorization(); err != nil {
		return "", err
	}
	if err := usecase.operation.CreateCredential(); err != nil {
		return "", err
	}
	if !usecase.operation.CredentialRecordValid() {
		return domain.PasskeyRecordInvalid, nil
	}
	if err := usecase.operation.RegisterCredential(); err != nil {
		return "", err
	}
	return "", usecase.operation.AdvanceSession()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
