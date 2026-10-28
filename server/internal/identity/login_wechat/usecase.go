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
func (usecase Usecase) Execute(operation Operation, input Input) error {
	if err := operation.ExchangeCode(input.Code); err != nil {
		return err
	}
	operation.InitializeAccount()
	if operation.Taken() {
		if err := operation.LoadAccount(); err != nil {
			return err
		}
		if operation.AccountID() == 0 {
			return domain.ErrAccountDeleted
		}
	} else {
		if !operation.RegistrationEnabled() {
			return domain.ErrRegistrationDisabled
		}
		operation.PrepareAccount()
		if err := operation.InsertAccount(input.Lang); err != nil {
			return err
		}
	}
	if !operation.AccountEnabled() {
		return domain.ErrAccountBanned
	}
	return nil
}

type Usecase struct{}

func NewUsecase() Usecase { return Usecase{} }
