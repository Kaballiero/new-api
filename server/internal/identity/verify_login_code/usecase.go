package verify_login_code

type VerifyLoginCodeOperation interface {
	RequireVerification() error
	LoadFactor() error
	VerifyFactor() error
	Complete() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.RequireVerification(); err != nil {
		return err
	}
	if err := usecase.operation.LoadFactor(); err != nil {
		return err
	}
	if err := usecase.operation.VerifyFactor(); err != nil {
		return err
	}
	return usecase.operation.Complete()
}

type Usecase struct {
	operation VerifyLoginCodeOperation
}

func NewUsecase(operation VerifyLoginCodeOperation) Usecase {
	return Usecase{operation: operation}
}
