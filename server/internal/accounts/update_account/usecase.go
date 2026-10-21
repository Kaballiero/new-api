package update_account

import "github.com/QuantumNous/new-api/internal/accounts/domain"

type Mutation interface {
	EnsureUsernameAvailable(string, int) error
	Edit(bool) (int64, error)
	SetPermissions(int, map[string]map[string]bool) error
	ClearAuthorization(int) error
}
type Operation interface {
	LoadOrigin(int) (domain.AdminTarget, error)
	UseOriginRole(int)
	EditTransaction(func(Mutation) error) error
	ReloadPolicy() error
	RevokeSessions(int, string) error
	PublishAuthCache(int) error
	AuditUpdated(domain.AdminTarget)
	ReloadAccount(int) (*domain.User, error)
	RequestedAccount() *domain.User
}

// Execute edits an account. LoadOrigin reports a missing account as
// domain.ErrAccountNotFound and other load failures as domain.Internal;
// EditTransaction reports a taken username as domain.ErrUsernameTaken. Other
// write and post-commit failures are returned unclassified.
func (scenario Usecase) Execute(input Input, actorRole int) (*domain.User, error) {
	op, roles := scenario.operation, scenario.roles

	origin, err := op.LoadOrigin(input.ID)
	if err != nil {
		return nil, err
	}
	if input.RequestedRole != roles.Guest && input.RequestedRole != origin.Role {
		return nil, domain.ErrRoleChangeNotAllowed
	}
	op.UseOriginRole(origin.Role)
	if !domain.CanManageTargetRole(actorRole, origin.Role, roles.Root) {
		return nil, domain.ErrManageForbidden
	}
	touched := false
	var authVersion int64
	err = op.EditTransaction(func(tx Mutation) error {
		if err := tx.EnsureUsernameAvailable(input.Username, input.ID); err != nil {
			return err
		}
		var err error
		authVersion, err = tx.Edit(input.ChangePassword)
		if err != nil {
			return err
		}
		touched, err = domain.UpdateAdminPermissions(tx, roles, actorRole, origin, input.Permissions)
		return err
	})
	if err != nil {
		return nil, err
	}
	if touched {
		if err := op.ReloadPolicy(); err != nil {
			return nil, err
		}
	}
	if authVersion > origin.AuthVersion {
		if err := op.RevokeSessions(input.ID, "admin_user_update"); err != nil {
			return nil, err
		}
	}
	if err := op.PublishAuthCache(input.ID); err != nil {
		return nil, err
	}
	op.AuditUpdated(origin)
	response, err := op.ReloadAccount(input.ID)
	if err != nil {
		response = op.RequestedAccount()
	}
	response.Password = ""
	return response, nil
}

type Usecase struct {
	operation Operation
	roles     domain.AdminRoles
}

func NewUsecase(operation Operation, roles domain.AdminRoles) Usecase {
	return Usecase{operation: operation, roles: roles}
}
