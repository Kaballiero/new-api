package complete_verification

type CompleteSecurityOperation interface {
	ValidateSession() error
	RequireMethod(method string) error
	IssueProof(method string) error
}

func (usecase Usecase) Execute(method string) error {
	if err := usecase.operation.ValidateSession(); err != nil {
		return err
	}
	if err := usecase.operation.RequireMethod(method); err != nil {
		return err
	}
	return usecase.operation.IssueProof(method)
}

type Usecase struct {
	operation CompleteSecurityOperation
}

func NewUsecase(operation CompleteSecurityOperation) Usecase {
	return Usecase{operation: operation}
}
