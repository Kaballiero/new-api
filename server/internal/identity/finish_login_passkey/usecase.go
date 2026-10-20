package finish_login_passkey

type Operation interface {
	RequireLoginVerification() error
	ParseAssertion() error
	PopBoundLoginFlow() error
	BoundFlowValid() bool
	InvalidFlow() error
	LoadCredential() error
	Build() error
	ValidateLogin() error
	UpdateAssertion() error
	CompleteLogin() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.RequireLoginVerification(); err != nil {
		return err
	}
	if err := usecase.operation.ParseAssertion(); err != nil {
		return err
	}
	if err := usecase.operation.PopBoundLoginFlow(); err != nil {
		return err
	}
	if !usecase.operation.BoundFlowValid() {
		return usecase.operation.InvalidFlow()
	}
	if err := usecase.operation.LoadCredential(); err != nil {
		return err
	}
	if err := usecase.operation.Build(); err != nil {
		return err
	}
	if err := usecase.operation.ValidateLogin(); err != nil {
		return err
	}
	if err := usecase.operation.UpdateAssertion(); err != nil {
		return err
	}
	return usecase.operation.CompleteLogin()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
