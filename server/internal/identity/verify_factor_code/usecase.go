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

// VerifyTwoFactorCode выбирает ровно один механизм и, следовательно, одну попытку счётчика ошибок.
func (usecase Usecase) Execute(code string) error {
	if !usecase.operation.Enabled() {
		return usecase.errors.TwoFANotEnabled()
	}
	if usecase.operation.Locked() {
		return usecase.errors.Locked()
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
		return usecase.errors.Failed()
	}
	return nil
}

type Usecase struct {
	operation FactorCodeOperation
	errors    domain.VerificationErrors
}

func NewUsecase(operation FactorCodeOperation, errors domain.VerificationErrors) Usecase {
	return Usecase{operation: operation, errors: errors}
}
