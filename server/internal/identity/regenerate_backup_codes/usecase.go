package regenerate_backup_codes

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	GenerateBackupCodes() error
	ReplaceBackupCodes() error
	AdvanceSession() error
}

// Execute replaces the backup codes. Generation and storage failures are
// *domain.LoggedError over domain.ErrBackupCodesGenerate and
// domain.ErrBackupCodesSave with the cause text; session errors are returned
// as is.
func (usecase Usecase) Execute() error {
	if err := usecase.operation.GenerateBackupCodes(); err != nil {
		return &domain.LoggedError{Err: domain.ErrBackupCodesGenerate, Log: err.Error()}
	}
	if err := usecase.operation.ReplaceBackupCodes(); err != nil {
		return &domain.LoggedError{Err: domain.ErrBackupCodesSave, Log: err.Error()}
	}
	return usecase.operation.AdvanceSession()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
