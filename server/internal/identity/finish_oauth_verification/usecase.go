package finish_oauth_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	FlowValid() bool
	FindLinkedAccount() error
	AccountMatches() bool
	IssueProof() error
}

func (usecase Usecase) Execute() error {
	if !usecase.operation.FlowValid() {
		return domain.ErrAuthTokenInvalid
	}
	if err := usecase.operation.FindLinkedAccount(); err != nil {
		return err
	}
	if !usecase.operation.AccountMatches() {
		return domain.ErrOAuthAccountMismatch
	}
	return usecase.operation.IssueProof()
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
