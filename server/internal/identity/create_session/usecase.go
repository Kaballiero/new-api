package create_session

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"time"
)

type CreateOperation interface {
	LoadAccount() (domain.AccountState, error)
	CountActive(now int64) (int64, error)
	CountIssued(since int64) (int64, error)
	Create(authVersion int64) error
	IssueBundle() error
	RevokeCreated(reason string) error
}

func (usecase Usecase) Execute(operation CreateOperation, expectedAuthVersion int64) error {
	user, err := operation.LoadAccount()
	if err != nil {
		return err
	}
	if user.Status != usecase.policy.EnabledStatus || user.AuthVersion <= 0 {
		return domain.ErrLoginSessionInvalid
	}
	if expectedAuthVersion > 0 && user.AuthVersion != expectedAuthVersion {
		return domain.ErrLoginSessionRevoked
	}
	now := time.Now().Unix()
	active, err := operation.CountActive(now)
	if err != nil {
		return err
	}
	if active >= usecase.policy.ActiveLimit() {
		return domain.ErrSessionLimit
	}
	issued, err := operation.CountIssued(now - usecase.policy.IssuanceWindowSeconds())
	if err != nil {
		return err
	}
	if issued >= usecase.policy.IssuanceLimit() {
		return domain.ErrSessionIssuanceLimit
	}
	if err := operation.Create(user.AuthVersion); err != nil {
		return err
	}
	if err := operation.IssueBundle(); err != nil {
		_ = operation.RevokeCreated("token_issue_failed")
		return err
	}
	return nil
}

type Usecase struct {
	policy domain.SessionPolicy
}

func NewUsecase(policy domain.SessionPolicy) Usecase {
	return Usecase{policy: policy}
}
