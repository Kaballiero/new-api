package start_oauth_verification

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
)

type Operation interface {
	Bind() error
	FindLinkedAccount() error
	Capture()
}

func (usecase Usecase) Execute(operation Operation) error {
	if err := operation.Bind(); err != nil {
		return err
	}
	if err := operation.FindLinkedAccount(); err != nil {
		return err
	}
	operation.Capture()
	return nil
}

func LinkedAccount(providers []identitydto.VerificationOAuthProvider, provider string) (string, error) {
	for _, linked := range providers {
		if linked.Slug == provider {
			return linked.UserID, nil
		}
	}
	return "", domain.ErrVerificationUnavailable
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
