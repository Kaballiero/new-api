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

func (usecase Usecase) Execute(operation SecurityInputOperation, scope string, method string) error {
	if err := operation.BindOperation(); err != nil {
		return err
	}
	if err := operation.RequireMethod(method); err != nil {
		return err
	}
	switch method {
	case "password":
		if operation.PasswordEncryptionEnabled() {
			if err := operation.DecryptPassword(); err != nil {
				return domain.ErrVerificationFailed
			}
		}
		if err := operation.LoadPassword(); err != nil {
			return err
		}
		if !operation.PasswordMatches() {
			return domain.ErrVerificationFailed
		}
	case "2fa":
		if scope == "2fa.backup_codes.regenerate" && !operation.NumericCodeValid() {
			return domain.ErrVerificationFailed
		}
		if err := operation.LoadFactor(); err != nil {
			return err
		}
		if err := operation.VerifyFactor(); err != nil {
			return err
		}
	case "passkey", "oauth":
		return domain.ErrVerificationFlowRequired
	default:
		return domain.ErrProofMethod
	}
	return operation.Complete()
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
