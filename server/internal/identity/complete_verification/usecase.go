package complete_verification

type CompleteSecurityOperation interface {
	ValidateSession() error
	RequireMethod(method string) error
	IssueProof(method string) error
}

func (usecase Usecase) Execute(operation CompleteSecurityOperation, method string) error {
	if err := operation.ValidateSession(); err != nil {
		return err
	}
	if err := operation.RequireMethod(method); err != nil {
		return err
	}
	return operation.IssueProof(method)
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
