package begin_passkey_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	BindOperation() error
	ReadIdentity() bool
	InvalidIdentity() error
	RequireMethod() error
	LoadCredential() error
	Build() error
	BeginLogin() error
	CreateVerificationFlow() error
}

func (usecase Usecase) Execute() (domain.PasskeyFailure, error) {
	if err := usecase.operation.BindOperation(); err != nil {
		return "", err
	}
	if !usecase.operation.ReadIdentity() {
		return "", usecase.operation.InvalidIdentity()
	}
	if err := usecase.operation.RequireMethod(); err != nil {
		return "", err
	}
	if err := usecase.operation.LoadCredential(); err != nil {
		return domain.PasskeyRecordMissing, nil
	}
	if err := usecase.operation.Build(); err != nil {
		return "", err
	}
	if err := usecase.operation.BeginLogin(); err != nil {
		return "", err
	}
	return "", usecase.operation.CreateVerificationFlow()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
