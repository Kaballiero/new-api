package send_verification_email

import "github.com/QuantumNous/new-api/internal/identity/domain"

type EmailVerificationOperation interface {
	// ValidateEmail returns domain.ErrEmailInvalid or domain.ErrEmailRestricted
	// for a rejected address.
	ValidateEmail(raw string) (string, error)
	EmailTaken(email string) bool
	GenerateCode(length int) string
	RegisterCode(email, code string)
	Deliver(email, code string) error
}

// Execute sends a registration verification code. An address used by another
// account is domain.ErrEmailTaken.
func (usecase Usecase) Execute(raw string) error {
	email, err := usecase.operation.ValidateEmail(raw)
	if err != nil {
		return err
	}
	if usecase.operation.EmailTaken(email) {
		return domain.ErrEmailTaken
	}
	code := usecase.operation.GenerateCode(6)
	usecase.operation.RegisterCode(email, code)
	return usecase.operation.Deliver(email, code)
}

type Usecase struct {
	operation EmailVerificationOperation
}

func NewUsecase(operation EmailVerificationOperation) Usecase {
	return Usecase{operation: operation}
}
