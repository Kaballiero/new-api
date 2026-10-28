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

func (usecase Usecase) Execute(operation CompleteLoginOperation, method string) error {
	if !operation.VerificationValid() {
		return domain.ErrAuthFlowInvalid
	}
	if err := operation.NewSession(); err != nil {
		return err
	}
	if err := operation.ConsumeAndCreate(func(flowID int64, payloadValid bool) error {
		if flowID != operation.ExpectedFlowID() || !payloadValid {
			return domain.ErrAuthFlowInvalid
		}
		return operation.RequireCurrentMethod(method)
	}); err != nil {
		return err
	}
	if err := operation.IssueBundle(); err != nil {
		_ = operation.RevokeCreated("token_issue_failed")
		return err
	}
	return nil
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
