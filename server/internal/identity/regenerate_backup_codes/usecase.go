package regenerate_backup_codes

type Operation interface {
	GenerateBackupCodes() error
	ReplaceBackupCodes() error
	AdvanceSession() error
}

func (usecase Usecase) Execute() (Failure, error) {
	if err := usecase.operation.GenerateBackupCodes(); err != nil {
		return GenerationFailed, err
	}
	if err := usecase.operation.ReplaceBackupCodes(); err != nil {
		return ReplacementFailed, err
	}
	if err := usecase.operation.AdvanceSession(); err != nil {
		return SessionFailed, err
	}
	return NoFailure, nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
