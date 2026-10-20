package request_password_reset

import "errors"

type PasswordResetEmailOperation interface {
	NormalizeEmail(raw string) string
	ValidEmail(email string) bool
	LookupAccount(email string) error
	GenerateCode(length int) string
	RegisterCode(email, code string)
	Deliver(email, code string) error
	LogDeliveryFailure(email string, err error)
	LogLookupFailure(email string, err error)
}

func (usecase Usecase) Execute(raw string) (invalidInput bool) {
	email := usecase.operation.NormalizeEmail(raw)
	if !usecase.operation.ValidEmail(email) {
		return true
	}
	if err := usecase.operation.LookupAccount(email); err != nil {
		if !errors.Is(err, usecase.emailNotFound()) {
			usecase.operation.LogLookupFailure(email, err)
		}
		return false
	}
	code := usecase.operation.GenerateCode(0)
	usecase.operation.RegisterCode(email, code)
	if err := usecase.operation.Deliver(email, code); err != nil {
		usecase.operation.LogDeliveryFailure(email, err)
	}
	return false
}

type Usecase struct {
	operation     PasswordResetEmailOperation
	emailNotFound func() error
}

func NewUsecase(operation PasswordResetEmailOperation, emailNotFound func() error) Usecase {
	return Usecase{operation: operation, emailNotFound: emailNotFound}
}
