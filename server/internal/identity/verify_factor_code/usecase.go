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
func (usecase Usecase) Execute(code string) error {
	if !usecase.operation.Enabled() {
		return domain.ErrTwoFANotEnabled
	}
	if usecase.operation.Locked() {
		return domain.ErrVerificationLocked
	}
	code = strings.TrimSpace(code)
	var valid bool
	var err error
	if numeric, numericErr := usecase.operation.NumericCode(code); numericErr == nil {
		valid, err = usecase.operation.ValidateTOTP(numeric)
	} else if usecase.operation.BackupCodeValid(code) {
		valid, err = usecase.operation.ValidateBackup(code)
	} else {
		err = usecase.operation.IncrementFailedAttempts()
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
	operation FactorCodeOperation
}

func NewUsecase(operation FactorCodeOperation) Usecase {
	return Usecase{operation: operation}
}
