package change_password

import "github.com/QuantumNous/new-api/internal/identity/domain"

// PasswordWriteOperation hashes before opening the transaction and keeps every
// credential decision inside one authorized transaction.
type PasswordWriteOperation interface {
	HashRequested() error
	Transaction(func(PasswordMutation) error) error
}

type PasswordMutation interface {
	LoadCurrent() (hasPassword bool, err error)
	OriginalMatches() bool
	RequestedMatches() bool
	IncrementAuthVersion() error
	UpdateAndReload() error
}

func (usecase Usecase) Execute(firstPassword bool) error {
	if err := usecase.operation.HashRequested(); err != nil {
		return err
	}
	return usecase.operation.Transaction(func(m PasswordMutation) error {
		hasPassword, err := m.LoadCurrent()
		if err != nil {
			return err
		}
		if firstPassword != !hasPassword {
			return domain.ErrPasswordState
		}
		if !firstPassword && !m.OriginalMatches() {
			return domain.ErrCurrentPasswordInvalid
		}
		if hasPassword && m.RequestedMatches() {
			return domain.ErrPasswordSame
		}
		if err := m.IncrementAuthVersion(); err != nil {
			return err
		}
		return m.UpdateAndReload()
	})
}

type Usecase struct {
	operation PasswordWriteOperation
}

func NewUsecase(operation PasswordWriteOperation) Usecase {
	return Usecase{operation: operation}
}
