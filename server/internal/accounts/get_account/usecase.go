package get_account

import (
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Operation interface {
	LoadOrigin(int) (domain.AdminTarget, error)
}

func (scenario Usecase) Execute(input Input) *accounts.AdminFailure {
	id, actorRole := input.ID, input.ActorRole
	op, rootRole := scenario.operation, scenario.rootRole
	target, err := op.LoadOrigin(id)
	if err != nil {
		return accounts.AdminLoadFailure(err)
	}
	if !domain.CanManageTargetRole(actorRole, target.Role, rootRole) {
		return &accounts.AdminFailure{Kind: accounts.AdminForbidden}
	}
	return nil
}

type Usecase struct {
	operation Operation
	rootRole  int
}

func NewUsecase(operation Operation, rootRole int) Usecase {
	return Usecase{operation: operation, rootRole: rootRole}
}
