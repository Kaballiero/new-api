package get_account

import "github.com/QuantumNous/new-api/internal/accounts/domain"

type Operation interface {
	LoadOrigin(int) (domain.AdminTarget, error)
}

// Execute checks that the actor may view the account. LoadOrigin reports a
// missing account as domain.ErrAccountNotFound and other load failures as
// domain.Internal.
func (scenario Usecase) Execute(input Input) error {
	id, actorRole := input.ID, input.ActorRole
	op, rootRole := scenario.operation, scenario.rootRole
	target, err := op.LoadOrigin(id)
	if err != nil {
		return err
	}
	if !domain.CanManageTargetRole(actorRole, target.Role, rootRole) {
		return domain.ErrViewForbidden
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
