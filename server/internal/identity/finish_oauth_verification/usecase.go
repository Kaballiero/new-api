package finish_oauth_verification

type Operation interface {
	FlowValid() bool
	InvalidError() error
	FindLinkedAccount() error
	AccountMatches() bool
	MismatchError() error
	IssueProof() error
}

func (usecase Usecase) Execute() error {
	if !usecase.operation.FlowValid() {
		return usecase.operation.InvalidError()
	}
	if err := usecase.operation.FindLinkedAccount(); err != nil {
		return err
	}
	if !usecase.operation.AccountMatches() {
		return usecase.operation.MismatchError()
	}
	return usecase.operation.IssueProof()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
