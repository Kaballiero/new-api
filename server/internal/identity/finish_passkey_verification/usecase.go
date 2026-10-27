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

func (usecase Usecase) Execute(operation Operation) error {
	if err := operation.Build(); err != nil {
		return err
	}
	if err := operation.LoadCredential(); err != nil {
		return domain.ErrPasskeyMissing
	}
	if !operation.ReadIdentity() {
		return domain.ErrPasskeyIdentityRequired
	}
	if err := operation.PopVerificationFlow(); err != nil {
		return err
	}
	if !operation.VerificationRequired() {
		return domain.ErrAuthFlowInvalid
	}
	if err := operation.ValidateLogin(); err != nil {
		return err
	}
	if err := operation.UpdateAssertion(); err != nil {
		return err
	}
	return operation.IssueProof()
}

type Usecase struct{}

func NewUsecase() Usecase { return Usecase{} }
