package require_verification_method

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type MethodOptions interface {
	MethodCount() int
	MethodOption(index int) (method string, available bool)
}

func (usecase Usecase) Execute(operation MethodOptions, method string) error {
	for i := range operation.MethodCount() {
		candidate, available := operation.MethodOption(i)
		if candidate != method {
			continue
		}
		if !available {
			return domain.ErrVerificationUnavailable
		}
		return nil
	}
	return domain.ErrProofMethod
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
