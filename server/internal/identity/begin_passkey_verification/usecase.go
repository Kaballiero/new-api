package begin_passkey_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	BindOperation() error
	ReadIdentity() bool
	RequireMethod() error
	LoadCredential() error
	Build() error
	BeginLogin() error
	CreateVerificationFlow() error
}

func (usecase Usecase) Execute(operation Operation) error {
	if err := operation.BindOperation(); err != nil {
		return err
	}
	if !operation.ReadIdentity() {
		return domain.ErrAuthTokenInvalid
	}
	if err := operation.RequireMethod(); err != nil {
		return err
	}
	if err := operation.LoadCredential(); err != nil {
		return domain.ErrPasskeyMissing
	}
	if err := operation.Build(); err != nil {
		return err
	}
	if err := operation.BeginLogin(); err != nil {
		return err
	}
	return operation.CreateVerificationFlow()
}

type Usecase struct{}

func NewUsecase() Usecase { return Usecase{} }
