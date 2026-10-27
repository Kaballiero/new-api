package verify_factor_code

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"strings"
)

type FactorCodeOperation interface {
	Enabled() bool
	Locked() bool
	NumericCode(code string) (string, error)
	BackupCodeValid(code string) bool
	ValidateTOTP(code string) (bool, error)
	ValidateBackup(code string) (bool, error)
	IncrementFailedAttempts() error
}

// Execute selects exactly one mechanism and therefore one failed-attempt counter increment.
func (usecase Usecase) Execute(operation FactorCodeOperation, code string) error {
	if !operation.Enabled() {
		return domain.ErrTwoFANotEnabled
	}
	if operation.Locked() {
		return domain.ErrVerificationLocked
	}
	code = strings.TrimSpace(code)
	var valid bool
	var err error
	if numeric, numericErr := operation.NumericCode(code); numericErr == nil {
		valid, err = operation.ValidateTOTP(numeric)
	} else if operation.BackupCodeValid(code) {
		valid, err = operation.ValidateBackup(code)
	} else {
		err = operation.IncrementFailedAttempts()
	}
	if err != nil {
		return err
	}
	if !valid {
		return domain.ErrVerificationFailed
	}
	return nil
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
