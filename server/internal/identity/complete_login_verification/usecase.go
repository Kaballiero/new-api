package complete_login_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type CompleteLoginOperation interface {
	VerificationValid() bool
	NewSession() error
	ConsumeAndCreate(check func(flowID int64, payloadValid bool) error) error
	ExpectedFlowID() int64
	RequireCurrentMethod(method string) error
	IssueBundle() error
	RevokeCreated(reason string) error
}

func (usecase Usecase) Execute(method string) error {
	if !usecase.operation.VerificationValid() {
		return domain.ErrAuthFlowInvalid
	}
	if err := usecase.operation.NewSession(); err != nil {
		return err
	}
	if err := usecase.operation.ConsumeAndCreate(func(flowID int64, payloadValid bool) error {
		if flowID != usecase.operation.ExpectedFlowID() || !payloadValid {
			return domain.ErrAuthFlowInvalid
		}
		return usecase.operation.RequireCurrentMethod(method)
	}); err != nil {
		return err
	}
	if err := usecase.operation.IssueBundle(); err != nil {
		_ = usecase.operation.RevokeCreated("token_issue_failed")
		return err
	}
	return nil
}

type Usecase struct {
	operation CompleteLoginOperation
}

func NewUsecase(operation CompleteLoginOperation) Usecase {
	return Usecase{operation: operation}
}
