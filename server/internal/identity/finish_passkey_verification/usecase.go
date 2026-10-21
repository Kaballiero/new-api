package finish_passkey_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	Build() error
	LoadCredential() error
	ReadIdentity() bool
	PopVerificationFlow() error
	VerificationRequired() bool
	ValidateLogin() error
	UpdateAssertion() error
	IssueProof() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.Build(); err != nil {
		return err
	}
	if err := usecase.operation.LoadCredential(); err != nil {
		return domain.ErrPasskeyMissing
	}
	if !usecase.operation.ReadIdentity() {
		return domain.ErrPasskeyIdentityRequired
	}
	if err := usecase.operation.PopVerificationFlow(); err != nil {
		return err
	}
	if !usecase.operation.VerificationRequired() {
		return domain.ErrAuthFlowInvalid
	}
	if err := usecase.operation.ValidateLogin(); err != nil {
		return err
	}
	if err := usecase.operation.UpdateAssertion(); err != nil {
		return err
	}
	return usecase.operation.IssueProof()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
