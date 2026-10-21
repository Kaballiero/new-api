package admin_unlink_oauth_account

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
)

type Operation interface {
	TargetRole(userID int) (int, error)
	ActorRole() int
	RootRole() int
	ProviderID() (int, error)
	DeleteBinding(userID, providerID int) error
}

func (usecase Usecase) Execute(input Input) error {
	role, err := usecase.operation.TargetRole(input.UserID)
	if err != nil {
		return err
	}
	if !domain.CanManageTargetRole(usecase.operation.ActorRole(), role, usecase.operation.RootRole()) {
		return identitydomain.ErrTargetForbidden
	}
	providerID, err := usecase.operation.ProviderID()
	if err != nil {
		return identitydomain.ErrProviderIDInvalid
	}
	return usecase.operation.DeleteBinding(input.UserID, providerID)
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
