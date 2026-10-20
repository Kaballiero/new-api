package change_password

// PasswordWriteOperation хеширует до открытия транзакции и сохраняет все
// решения об учётных данных внутри одной авторизованной транзакции.
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

type PasswordWriteErrors struct{ State, CurrentInvalid, Same func() error }

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
			return usecase.errors.State()
		}
		if !firstPassword && !m.OriginalMatches() {
			return usecase.errors.CurrentInvalid()
		}
		if hasPassword && m.RequestedMatches() {
			return usecase.errors.Same()
		}
		if err := m.IncrementAuthVersion(); err != nil {
			return err
		}
		return m.UpdateAndReload()
	})
}

type Usecase struct {
	operation PasswordWriteOperation
	errors    PasswordWriteErrors
}

func NewUsecase(operation PasswordWriteOperation, errors PasswordWriteErrors) Usecase {
	return Usecase{operation: operation, errors: errors}
}
