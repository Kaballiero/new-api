package unlink_oauth_account

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Mutation interface {
	EnabledMethods() domain.LoginMethods
	Unlink(domain.LoginMethods) error
}

func ExecuteMutation(op Mutation) error { methods := op.EnabledMethods(); return op.Unlink(methods) }

type Confirmation interface {
	Unlink() error
	MarkSucceeded()
	LoadAccount() error
	NotifyAccount() bool
	SetNotificationWarning(bool)
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.Unlink(); err != nil {
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
	operation Confirmation
}

func NewUsecase(operation Confirmation) Usecase {
	return Usecase{operation: operation}
}
