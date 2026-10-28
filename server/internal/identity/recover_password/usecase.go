package recover_password

import "github.com/QuantumNous/new-api/internal/identity/domain"

type PasswordRecoveryOperation interface {
	NormalizeEmail(raw string) string
	VerifyCode(email, token string) bool
	GeneratePassword() string
	ResetPassword(email, password string) error
	// EmailUnresolved reports that ResetPassword found no single account.
	EmailUnresolved(error) bool
	DeleteCode(email string)
}

// Execute resets the password of the reset link and returns the new password.
// Missing input is domain.ErrInvalidInput; a wrong code or an address without
// a single account is domain.ErrRecoveryLinkInvalid.
func (usecase Usecase) Execute(email string, token string) (string, error) {
	email = usecase.operation.NormalizeEmail(email)
	if email == "" || token == "" {
		return "", domain.ErrInvalidInput
	}
	if !usecase.operation.VerifyCode(email, token) {
		return "", domain.ErrRecoveryLinkInvalid
	}
	password := usecase.operation.GeneratePassword()
	if err := usecase.operation.ResetPassword(email, password); err != nil {
		if usecase.operation.EmailUnresolved(err) {
			return "", domain.ErrRecoveryLinkInvalid
		}
		return "", err
	}
	usecase.operation.DeleteCode(email)
	return password, nil
}

type Usecase struct {
	operation PasswordRecoveryOperation
}

func NewUsecase(operation PasswordRecoveryOperation) Usecase {
	return Usecase{operation: operation}
}
