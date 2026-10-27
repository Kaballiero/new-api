package verify_login_code

type VerifyLoginCodeOperation interface {
	RequireVerification() error
	LoadFactor() error
	VerifyFactor() error
	Complete() error
}

func (usecase Usecase) Execute(operation VerifyLoginCodeOperation) error {
	if err := operation.RequireVerification(); err != nil {
		return err
	}
	if err := operation.LoadFactor(); err != nil {
		return err
	}
	if err := operation.VerifyFactor(); err != nil {
		return err
	}
	return operation.Complete()
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
