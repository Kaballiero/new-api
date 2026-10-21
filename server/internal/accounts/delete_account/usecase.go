package delete_account

import "github.com/QuantumNous/new-api/internal/accounts/domain"

type Operation interface {
	LoadOrigin(int) (domain.AdminTarget, error)
	HardDelete(int) error
	AuditDeleted(domain.AdminTarget)
}

// Execute checks roles strictly, even when the actor is root. LoadOrigin
// reports a missing account as domain.ErrAccountNotFound and other load
// failures as domain.Internal.
func (scenario Usecase) Execute(input Input) error {
	id, actorRole := input.ID, input.ActorRole
	op := scenario.operation
	target, err := op.LoadOrigin(id)
	if err != nil {
		return err
	}
	if actorRole <= target.Role {
		return domain.ErrManageForbidden
	}
	if err := op.HardDelete(id); err != nil {
		return domain.Internal(err)
	}
	op.AuditDeleted(target)
	return nil
}

type Usecase struct{ operation Operation }

func NewUsecase(operation Operation) Usecase { return Usecase{operation: operation} }
