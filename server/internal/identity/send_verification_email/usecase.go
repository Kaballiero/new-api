package send_verification_email

import "github.com/QuantumNous/new-api/internal/identity/domain"

type EmailVerificationOperation interface {
	// ValidateEmail returns domain.ErrEmailInvalid or domain.ErrEmailRestricted
	// for a rejected address.
	ValidateEmail(raw string) (string, error)
	EmailTaken(email string) bool
	GenerateCode(length int) string
	RegisterCode(email, code string)
	// Deliver sends the code in the language lang.
	Deliver(email, code, lang string) error
}

// Execute sends a registration verification code. An address used by another
// account is domain.ErrEmailTaken.
func (usecase Usecase) Execute(input Input) error {
	email, err := usecase.operation.ValidateEmail(input.Email)
	if err != nil {
		return err
	}
	if usecase.operation.EmailTaken(email) {
		return domain.ErrEmailTaken
	}
	code := usecase.operation.GenerateCode(6)
	usecase.operation.RegisterCode(email, code)
	return usecase.operation.Deliver(email, code, input.Lang)
}

type Usecase struct {
	operation EmailVerificationOperation
}

func NewUsecase(operation EmailVerificationOperation) Usecase {
	return Usecase{operation: operation}
}
