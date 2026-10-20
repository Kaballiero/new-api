package require_verification_method

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type MethodOptions interface {
	MethodCount() int
	MethodOption(index int) (method string, available bool)
}

func (usecase Usecase) Execute(method string) error {
	for i := range usecase.operation.MethodCount() {
		candidate, available := usecase.operation.MethodOption(i)
		if candidate != method {
			continue
		}
		if !available {
			return usecase.errors.Unavailable()
		}
		return nil
	}
	return usecase.errors.Method()
}

type Usecase struct {
	operation MethodOptions
	errors    domain.VerificationErrors
}

func NewUsecase(operation MethodOptions, errors domain.VerificationErrors) Usecase {
	return Usecase{operation: operation, errors: errors}
}
