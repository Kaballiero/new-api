package delete_account

import (
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Operation interface {
	LoadOrigin(int) (domain.AdminTarget, error)
	HardDelete(int) error
	AuditDeleted(domain.AdminTarget)
}

// Run строго проверяет роли, даже если вызывающий — root.
func (scenario Usecase) Execute(input Input) *accounts.AdminFailure {
	id, actorRole := input.ID, input.ActorRole
	op := scenario.operation
	target, err := op.LoadOrigin(id)
	if err != nil {
		return accounts.AdminLoadFailure(err)
	}
	if actorRole <= target.Role {
		return &accounts.AdminFailure{Kind: accounts.AdminForbidden}
	}
	if err := op.HardDelete(id); err != nil {
		return &accounts.AdminFailure{Kind: accounts.AdminOrdinaryWriteFailed, Cause: err}
	}
	op.AuditDeleted(target)
	return nil
}

type Usecase struct{ operation Operation }

func NewUsecase(operation Operation) Usecase { return Usecase{operation: operation} }
