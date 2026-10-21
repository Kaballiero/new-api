package login_wechat

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	ExchangeCode(string) error
	InitializeAccount()
	Taken() bool
	LoadAccount() error
	AccountID() int
	RegistrationEnabled() bool
	PrepareAccount()
	// InsertAccount registers the account with persisted texts in lang.
	InsertAccount(lang string) error
	AccountEnabled() bool
}

// Execute signs in or registers the WeChat account of the code. Outcomes with
// their own responses are domain.ErrAccountDeleted,
// domain.ErrRegistrationDisabled and domain.ErrAccountBanned.
func (usecase Usecase) Execute(input Input) error {
	if err := usecase.operation.ExchangeCode(input.Code); err != nil {
		return err
	}
	usecase.operation.InitializeAccount()
	if usecase.operation.Taken() {
		if err := usecase.operation.LoadAccount(); err != nil {
			return err
		}
		if usecase.operation.AccountID() == 0 {
			return domain.ErrAccountDeleted
		}
	} else {
		if !usecase.operation.RegistrationEnabled() {
			return domain.ErrRegistrationDisabled
		}
		usecase.operation.PrepareAccount()
		if err := usecase.operation.InsertAccount(input.Lang); err != nil {
			return err
		}
	}
	if !usecase.operation.AccountEnabled() {
		return domain.ErrAccountBanned
	}
	return nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
