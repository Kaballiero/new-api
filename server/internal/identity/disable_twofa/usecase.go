package disable_twofa

type Operation interface {
	DisableFactor() error
	AdvanceSession() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.DisableFactor(); err != nil {
		return err
	}
	return usecase.operation.AdvanceSession()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
