package admin_disable_twofa

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
)

type Operation interface {
	// LoadTarget returns identitydomain.ErrTargetNotFound for a missing user.
	LoadTarget(id int) (role int, err error)
	ActorRole() int
	RootRole() int
	Disable(id int) error
	RevokeAll(id int, reason string) error
}

func (usecase Usecase) Execute(id int) error {
	role, err := usecase.operation.LoadTarget(id)
	if err != nil {
		return err
	}
	if !domain.CanManageTargetRole(usecase.operation.ActorRole(), role, usecase.operation.RootRole()) {
		return identitydomain.ErrTargetForbidden
	}
	if err := usecase.operation.Disable(id); err != nil {
		if errors.Is(err, identitydomain.ErrTwoFANotEnabled) {
			return identitydomain.ErrTargetTwoFADisabled
		}
		return err
	}
	return usecase.operation.RevokeAll(id, "admin_twofa_disabled")
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
