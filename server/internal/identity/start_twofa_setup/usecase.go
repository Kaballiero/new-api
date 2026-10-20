package start_twofa_setup

type StartTwoFASetupOperation interface {
	ValidateAuthorization() error
	LoadAccountName() (string, error)
	GenerateSecret(username string) error
	GenerateBackupCodes() error
	CreateEnrollment() error
	CaptureSetup()
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.ValidateAuthorization(); err != nil {
		return err
	}
	username, err := usecase.operation.LoadAccountName()
	if err != nil {
		return err
	}
	if err := usecase.operation.GenerateSecret(username); err != nil {
		return err
	}
	if err := usecase.operation.GenerateBackupCodes(); err != nil {
		return err
	}
	if err := usecase.operation.CreateEnrollment(); err != nil {
		return err
	}
	usecase.operation.CaptureSetup()
	return nil
}

type Usecase struct {
	operation StartTwoFASetupOperation
}

func NewUsecase(operation StartTwoFASetupOperation) Usecase {
	return Usecase{operation: operation}
}
