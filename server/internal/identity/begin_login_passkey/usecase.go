package begin_login_passkey

type Operation interface {
	RequireLoginVerification() error
	LoadCredential() error
	Build() error
	BeginLogin() error
	CreateBoundLoginFlow() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.RequireLoginVerification(); err != nil {
		return err
	}
	if err := usecase.operation.LoadCredential(); err != nil {
		return err
	}
	if err := usecase.operation.Build(); err != nil {
		return err
	}
	if err := usecase.operation.BeginLogin(); err != nil {
		return err
	}
	return usecase.operation.CreateBoundLoginFlow()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
