package send_verification_email

type EmailVerificationOperation interface {
	ValidateEmail(raw string) (string, error)
	EmailTaken(email string) bool
	GenerateCode(length int) string
	RegisterCode(email, code string)
	Deliver(email, code string) error
}

func (usecase Usecase) Execute(raw string) (EmailVerificationResult, error) {
	email, err := usecase.operation.ValidateEmail(raw)
	if err != nil {
		return EmailVerificationResult{ValidationRejected: true}, err
	}
	if usecase.operation.EmailTaken(email) {
		return EmailVerificationResult{EmailTaken: true}, nil
	}
	code := usecase.operation.GenerateCode(6)
	usecase.operation.RegisterCode(email, code)
	return EmailVerificationResult{}, usecase.operation.Deliver(email, code)
}

type Usecase struct {
	operation EmailVerificationOperation
}

func NewUsecase(operation EmailVerificationOperation) Usecase {
	return Usecase{operation: operation}
}
