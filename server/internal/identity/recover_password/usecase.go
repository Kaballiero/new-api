package recover_password

import "errors"

type PasswordRecoveryOperation interface {
	NormalizeEmail(raw string) string
	VerifyCode(email, token string) bool
	GeneratePassword() string
	ResetPassword(email, password string) error
	DeleteCode(email string)
}

type PasswordRecoveryErrors struct{ EmailNotFound, EmailAmbiguous func() error }

func (usecase Usecase) Execute(email string, token string) (PasswordRecoveryResult, error) {
	email = usecase.operation.NormalizeEmail(email)
	if email == "" || token == "" {
		return PasswordRecoveryResult{InvalidInput: true}, nil
	}
	if !usecase.operation.VerifyCode(email, token) {
		return PasswordRecoveryResult{InvalidLink: true}, nil
	}
	password := usecase.operation.GeneratePassword()
	if err := usecase.operation.ResetPassword(email, password); err != nil {
		if errors.Is(err, usecase.errors.EmailNotFound()) || errors.Is(err, usecase.errors.EmailAmbiguous()) {
			return PasswordRecoveryResult{InvalidLink: true}, nil
		}
		return PasswordRecoveryResult{}, err
	}
	usecase.operation.DeleteCode(email)
	return PasswordRecoveryResult{Password: password}, nil
}

type Usecase struct {
	operation PasswordRecoveryOperation
	errors    PasswordRecoveryErrors
}

func NewUsecase(operation PasswordRecoveryOperation, errors PasswordRecoveryErrors) Usecase {
	return Usecase{operation: operation, errors: errors}
}
