package verify_security_input

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type SecurityInputOperation interface {
	BindOperation() error
	RequireMethod(method string) error
	PasswordEncryptionEnabled() bool
	DecryptPassword() error
	LoadPassword() error
	PasswordMatches() bool
	NumericCodeValid() bool
	LoadFactor() error
	VerifyFactor() error
	Complete() error
}

func (usecase Usecase) Execute(scope string, method string) error {
	if err := usecase.operation.BindOperation(); err != nil {
		return err
	}
	if err := usecase.operation.RequireMethod(method); err != nil {
		return err
	}
	switch method {
	case "password":
		if usecase.operation.PasswordEncryptionEnabled() {
			if err := usecase.operation.DecryptPassword(); err != nil {
				return usecase.errors.Failed()
			}
		}
		if err := usecase.operation.LoadPassword(); err != nil {
			return err
		}
		if !usecase.operation.PasswordMatches() {
			return usecase.errors.Failed()
		}
	case "2fa":
		if scope == "2fa.backup_codes.regenerate" && !usecase.operation.NumericCodeValid() {
			return usecase.errors.Failed()
		}
		if err := usecase.operation.LoadFactor(); err != nil {
			return err
		}
		if err := usecase.operation.VerifyFactor(); err != nil {
			return err
		}
	case "passkey", "oauth":
		return usecase.errors.FlowRequired()
	default:
		return usecase.errors.Method()
	}
	return usecase.operation.Complete()
}

type Usecase struct {
	operation SecurityInputOperation
	errors    domain.VerificationErrors
}

func NewUsecase(operation SecurityInputOperation, errors domain.VerificationErrors) Usecase {
	return Usecase{operation: operation, errors: errors}
}
