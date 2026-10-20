package create_account

import (
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Mutation interface {
	Insert(NewAccount) (domain.AdminTarget, error)
	SetPermissions(int, map[string]map[string]bool) error
	ClearAuthorization(int) error
}
type Operation interface {
	CreateTransaction(func(Mutation) error) error
	ReloadPolicy() error
	FinishInsert()
	AuditCreated(domain.AdminTarget)
}

func (scenario Usecase) Execute(input Input, actorRole int) *accounts.AdminFailure {
	op, roles := scenario.operation, scenario.roles

	if input.DisplayName == "" {
		input.DisplayName = input.Username
	}
	if input.Role >= actorRole {
		return &accounts.AdminFailure{Kind: accounts.AdminCreateForbidden}
	}
	clean := NewAccount{Username: input.Username, Password: input.Password, DisplayName: input.DisplayName, Role: input.Role}
	var target domain.AdminTarget
	touched := false
	err := op.CreateTransaction(func(tx Mutation) error {
		var err error
		target, err = tx.Insert(clean)
		if err != nil {
			return err
		}
		touched, err = domain.UpdateAdminPermissions(tx, roles, actorRole, target, input.Permissions)
		return err
	})
	if err != nil {
		return &accounts.AdminFailure{Kind: accounts.AdminWriteFailed, Cause: err}
	}
	if touched {
		if err := op.ReloadPolicy(); err != nil {
			return &accounts.AdminFailure{Kind: accounts.AdminPostCommitFailed, Cause: err}
		}
	}
	op.FinishInsert()
	op.AuditCreated(target)
	return nil
}

type Usecase struct {
	operation Operation
	roles     domain.AdminRoles
}

func NewUsecase(operation Operation, roles domain.AdminRoles) Usecase {
	return Usecase{operation: operation, roles: roles}
}
