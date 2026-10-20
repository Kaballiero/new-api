package finish_passkey_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	Build() error
	LoadCredential() error
	ReadIdentity() bool
	PopVerificationFlow() error
	VerificationRequired() bool
	InvalidFlow() error
	ValidateLogin() error
	UpdateAssertion() error
	IssueProof() error
}

func (usecase Usecase) Execute() (domain.PasskeyFailure, error) {
	if err := usecase.operation.Build(); err != nil {
		return "", err
	}
	if err := usecase.operation.LoadCredential(); err != nil {
		return domain.PasskeyRecordMissing, nil
	}
	if !usecase.operation.ReadIdentity() {
		return domain.PasskeyIdentityRequired, nil
	}
	if err := usecase.operation.PopVerificationFlow(); err != nil {
		return "", err
	}
	if !usecase.operation.VerificationRequired() {
		return "", usecase.operation.InvalidFlow()
	}
	if err := usecase.operation.ValidateLogin(); err != nil {
		return "", err
	}
	if err := usecase.operation.UpdateAssertion(); err != nil {
		return "", err
	}
	return "", usecase.operation.IssueProof()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
