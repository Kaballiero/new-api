package request_password_reset

import "github.com/QuantumNous/new-api/internal/identity/domain"

type PasswordResetEmailOperation interface {
	NormalizeEmail(raw string) string
	ValidEmail(email string) bool
	LookupAccount(email string) error
	// EmailNotFound reports that LookupAccount found no account.
	EmailNotFound(error) bool
	GenerateCode(length int) string
	RegisterCode(email, code string)
	Deliver(email, code string) error
	LogDeliveryFailure(email string, err error)
	LogLookupFailure(email string, err error)
}

// Execute sends a password reset code. Only an invalid address is reported
// (domain.ErrInvalidInput): unknown accounts and delivery failures are logged
// so that the response does not reveal registered addresses.
func (usecase Usecase) Execute(raw string) error {
	email := usecase.operation.NormalizeEmail(raw)
	if !usecase.operation.ValidEmail(email) {
		return domain.ErrInvalidInput
	}
	if err := usecase.operation.LookupAccount(email); err != nil {
		if !usecase.operation.EmailNotFound(err) {
			usecase.operation.LogLookupFailure(email, err)
		}
		return nil
	}
	code := usecase.operation.GenerateCode(0)
	usecase.operation.RegisterCode(email, code)
	if err := usecase.operation.Deliver(email, code); err != nil {
		usecase.operation.LogDeliveryFailure(email, err)
	}
	return nil
}

type Usecase struct {
	operation PasswordResetEmailOperation
}

func NewUsecase(operation PasswordResetEmailOperation) Usecase {
	return Usecase{operation: operation}
}
