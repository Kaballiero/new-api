package delete_passkey

type Operation interface {
	DeleteCredential() error
	AdvanceSession() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.DeleteCredential(); err != nil {
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
