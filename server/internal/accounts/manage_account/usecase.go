package manage_account

import "github.com/QuantumNous/new-api/internal/accounts/domain"

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

// Execute applies a management action. LoadUnscoped reports a missing account
// as domain.ErrAccountNotFound and other load failures as domain.Internal;
// failed demotion and post-commit effects are returned unclassified.
func (scenario Usecase) Execute(input Input, actorRole int) (Result, error) {
	op, roles := scenario.operation, scenario.roles

	target, err := op.LoadUnscoped(input.ID)
	if err != nil {
		return Result{}, err
	}
	if !domain.CanManageTargetRole(actorRole, target.Role, roles.Root) {
		return Result{}, domain.ErrManageForbidden
	}
	switch input.Action {
	case "disable":
		if target.Role == roles.Root {
			return Result{}, domain.ErrCannotDisableRoot
		}
		op.UseStatus(roles.Disabled)
	case "enable":
		op.UseStatus(roles.Enabled)
	case "delete":
		if target.Role == roles.Root {
			return Result{}, domain.ErrCannotDeleteRoot
		}
		if err := op.Delete(); err != nil {
			return Result{}, domain.Internal(err)
		}
		if err := op.InvalidateTokens(target.ID); err != nil {
			op.LogTokenInvalidationFailure(target.ID, err)
		}
		op.AuditManaged(target, input.Action)
		return Result{Action: input.Action, UserID: target.ID, Deleted: true}, nil
	case "promote":
		if actorRole != roles.Root {
			return Result{}, domain.ErrAdminCannotPromote
		}
		if target.Role >= roles.Admin {
			return Result{}, domain.ErrAlreadyAdmin
		}
		op.UseRole(roles.Admin)
	case "demote":
		if target.Role == roles.Root {
			return Result{}, domain.ErrCannotDemoteRoot
		}
		if target.Role == roles.Common {
			return Result{}, domain.ErrAlreadyCommon
		}
		op.UseRole(roles.Common)
	default:
		return Result{}, domain.ErrInvalidAction
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
			return Result{}, err
		}
		if err := op.ReloadPolicy(); err != nil {
			return Result{}, err
		}
		if err := op.PublishAuthCache(target.ID); err != nil {
			return Result{}, err
		}
		if err := op.RevokeSessions(target.ID, "admin_demote"); err != nil {
			return Result{}, err
		}
	} else {
		target, err = op.Update()
		if err != nil {
			return Result{}, domain.Internal(err)
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
