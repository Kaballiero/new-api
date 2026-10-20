package advance_session_security

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Advancement interface {
	Advance(version int64) error
	RevokeOthers() error
	Issue() error
}
type BumpOperation interface {
	Advancement
	BumpUserVersion() (int64, error)
}
type CurrentVersionOperation interface {
	Advancement
	LoadAccount() (domain.AccountState, error)
	RevokedError() error
}

func (usecase Usecase) Execute(version int64) error {
	if err := usecase.operation.Advance(version); err != nil {
		return err
	}
	if err := usecase.operation.RevokeOthers(); err != nil {
		return err
	}
	return usecase.operation.Issue()
}
func BumpAndAdvance(op BumpOperation) error {
	version, err := op.BumpUserVersion()
	if err != nil {
		return err
	}
	return NewUsecase(op).Execute(version)
}
func AdvanceFromCurrentVersion(expectedVersion int64, enabledStatus int, op CurrentVersionOperation) error {
	user, err := op.LoadAccount()
	if err != nil {
		return err
	}
	if user.Status != enabledStatus || user.AuthVersion <= expectedVersion {
		return op.RevokedError()
	}
	return NewUsecase(op).Execute(user.AuthVersion)
}

type Usecase struct {
	operation Advancement
}

func NewUsecase(operation Advancement) Usecase {
	return Usecase{operation: operation}
}
