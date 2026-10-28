package start_twofa_setup

type StartTwoFASetupOperation interface {
	ValidateAuthorization() error
	LoadAccountName() (string, error)
	GenerateSecret(username string) error
	GenerateBackupCodes() error
	CreateEnrollment() error
	CaptureSetup()
}

func (usecase Usecase) Execute(operation StartTwoFASetupOperation) error {
	if err := operation.ValidateAuthorization(); err != nil {
		return err
	}
	username, err := operation.LoadAccountName()
	if err != nil {
		return err
	}
	if err := operation.GenerateSecret(username); err != nil {
		return err
	}
	if err := operation.GenerateBackupCodes(); err != nil {
		return err
	}
	if err := operation.CreateEnrollment(); err != nil {
		return err
	}
	operation.CaptureSetup()
	return nil
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
