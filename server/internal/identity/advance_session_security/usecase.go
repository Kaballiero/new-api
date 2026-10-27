package advance_session_security

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	LoadAccount() (domain.AccountState, error)
	Advance(int64) error
	RevokeOthers() error
	Issue() error
}

type Usecase struct{ enabledStatus int }

func NewUsecase(enabledStatus int) Usecase { return Usecase{enabledStatus: enabledStatus} }

func (u Usecase) Execute(op Operation, expectedVersion int64) error {
	user, err := op.LoadAccount()
	if err != nil {
		return err
	}
	if user.Status != u.enabledStatus || user.AuthVersion <= expectedVersion {
		return domain.ErrLoginSessionRevoked
	}
	if err := op.Advance(user.AuthVersion); err != nil {
		return err
	}
	if err := op.RevokeOthers(); err != nil {
		return err
	}
	return op.Issue()
}
