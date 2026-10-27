package finish_oauth_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	FlowValid() bool
	FindLinkedAccount() error
	AccountMatches() bool
	IssueProof() error
}

func (usecase Usecase) Execute(operation Operation) error {
	if !operation.FlowValid() {
		return domain.ErrAuthTokenInvalid
	}
	if err := operation.FindLinkedAccount(); err != nil {
		return err
	}
	if !operation.AccountMatches() {
		return domain.ErrOAuthAccountMismatch
	}
	return operation.IssueProof()
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
