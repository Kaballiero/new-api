package bind_wechat

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	ExchangeCode(string) error
	Taken() bool
	BindAtomically() error
	MarkSucceeded()
	LoadAccount() error
	NotifyAccount() bool
	SetNotificationWarning(bool)
}

// Execute binds the WeChat account of the code. A failed code exchange is a
// *domain.ProviderError; an account bound to another user is
// domain.ErrWeChatTaken.
func (usecase Usecase) Execute(input Input) error {
	if err := usecase.operation.ExchangeCode(input.Code); err != nil {
		return &domain.ProviderError{Cause: err}
	}
	if usecase.operation.Taken() {
		return domain.ErrWeChatTaken
	}
	if err := usecase.operation.BindAtomically(); err != nil {
		return err
	}
	usecase.operation.MarkSucceeded()
	if err := usecase.operation.LoadAccount(); err != nil {
		return err
	}
	usecase.operation.SetNotificationWarning(usecase.operation.NotifyAccount())
	return nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
