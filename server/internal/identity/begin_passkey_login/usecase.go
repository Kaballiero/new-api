package begin_passkey_login

type Operation interface {
	Build() error
	BeginDiscoverable() error
	CreateLoginFlow() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.Build(); err != nil {
		return err
	}
	if err := usecase.operation.BeginDiscoverable(); err != nil {
		return err
	}
	return usecase.operation.CreateLoginFlow()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
