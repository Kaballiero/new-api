package manage_account

import (
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type DemoteMutation interface {
	Update() (domain.AdminTarget, error)
	ClearAuthorization(int) error
}
type Operation interface {
	LoadUnscoped(int) (domain.AdminTarget, error)
	UseRole(int)
	UseStatus(int)
	Update() (domain.AdminTarget, error)
	DemoteTransaction(func(DemoteMutation) error) error
	ReloadPolicy() error
	PublishAuthCache(int) error
	RevokeSessions(int, string) error
	Delete() error
	InvalidateTokens(int) error
	LogTokenInvalidationFailure(int, error)
	AuditManaged(domain.AdminTarget, string)
}

func (scenario Usecase) Execute(input Input, actorRole int) (Result, *accounts.AdminFailure) {
	op, roles := scenario.operation, scenario.roles

	target, err := op.LoadUnscoped(input.ID)
	if err != nil {
		return Result{}, accounts.AdminLoadFailure(err)
	}
	if !domain.CanManageTargetRole(actorRole, target.Role, roles.Root) {
		return Result{}, &accounts.AdminFailure{Kind: accounts.AdminForbidden}
	}
	switch input.Action {
	case "disable":
		if target.Role == roles.Root {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminCannotDisableRoot}
		}
		op.UseStatus(roles.Disabled)
	case "enable":
		op.UseStatus(roles.Enabled)
	case "delete":
		if target.Role == roles.Root {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminCannotDeleteRoot}
		}
		if err := op.Delete(); err != nil {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminOrdinaryWriteFailed, Cause: err}
		}
		if err := op.InvalidateTokens(target.ID); err != nil {
			op.LogTokenInvalidationFailure(target.ID, err)
		}
		op.AuditManaged(target, input.Action)
		return Result{Action: input.Action, UserID: target.ID, Deleted: true}, nil
	case "promote":
		if actorRole != roles.Root {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminCannotPromote}
		}
		if target.Role >= roles.Admin {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminAlreadyAdmin}
		}
		op.UseRole(roles.Admin)
	case "demote":
		if target.Role == roles.Root {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminCannotDemoteRoot}
		}
		if target.Role == roles.Common {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminAlreadyCommon}
		}
		op.UseRole(roles.Common)
	default:
		return Result{}, &accounts.AdminFailure{Kind: accounts.AdminInvalidAction}
	}
	if input.Action == "demote" {
		err = op.DemoteTransaction(func(tx DemoteMutation) error {
			var err error
			target, err = tx.Update()
			if err != nil {
				return err
			}
			return tx.ClearAuthorization(target.ID)
		})
		if err != nil {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminWriteFailed, Cause: err}
		}
		if err := op.ReloadPolicy(); err != nil {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminPostCommitFailed, Cause: err}
		}
		if err := op.PublishAuthCache(target.ID); err != nil {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminPostCommitFailed, Cause: err}
		}
		if err := op.RevokeSessions(target.ID, "admin_demote"); err != nil {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminPostCommitFailed, Cause: err}
		}
	} else {
		target, err = op.Update()
		if err != nil {
			return Result{}, &accounts.AdminFailure{Kind: accounts.AdminOrdinaryWriteFailed, Cause: err}
		}
	}
	if err := op.InvalidateTokens(target.ID); err != nil {
		op.LogTokenInvalidationFailure(target.ID, err)
	}
	op.AuditManaged(target, input.Action)
	return Result{Action: input.Action, UserID: target.ID, Role: target.Role, Status: target.Status}, nil
}

type Usecase struct {
	operation Operation
	roles     domain.AdminRoles
}

func NewUsecase(operation Operation, roles domain.AdminRoles) Usecase {
	return Usecase{operation: operation, roles: roles}
}
