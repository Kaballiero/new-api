package domain

import "fmt"

type AdminTarget struct {
	ID          int
	Username    string
	Role        int
	Status      int
	AuthVersion int64
}

type AdminRoles struct {
	Guest, Common, Admin, Root int
	Enabled, Disabled          int
}
type AdminPermissionMutation interface {
	SetPermissions(int, map[string]map[string]bool) error
	ClearAuthorization(int) error
}

func UpdateAdminPermissions(m AdminPermissionMutation, roles AdminRoles, actorRole int, target AdminTarget, permissions map[string]map[string]bool) (bool, error) {
	if permissions == nil {
		if target.Role < roles.Admin && actorRole == roles.Root {
			return true, m.ClearAuthorization(target.ID)
		}
		return false, nil
	}
	if actorRole != roles.Root {
		return false, fmt.Errorf("only root can update admin permissions")
	}
	if target.Role < roles.Admin {
		return true, m.ClearAuthorization(target.ID)
	}
	return true, m.SetPermissions(target.ID, permissions)
}
