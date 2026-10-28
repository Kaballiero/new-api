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

func (usecase Usecase) Execute(operation Operation) error {
	if err := operation.Build(); err != nil {
		return err
	}
	if err := operation.PopLoginFlow(); err != nil {
		return err
	}
	if !operation.VerificationRequired() {
		return domain.ErrAuthFlowInvalid
	}
	if err := operation.ValidateDiscoverable(); err != nil {
		return err
	}
	if !operation.LoginAccountValid() {
		return domain.ErrPasskeyLoginInvalid
	}
	if !operation.LoginAccountEnabled() {
		return domain.ErrPasskeyAccountDisabled
	}
	return operation.UpdateAssertion()
}

type Usecase struct{}

func NewUsecase() Usecase { return Usecase{} }
