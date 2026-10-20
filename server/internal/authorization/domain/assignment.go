package domain

func ResolveSystemRoles(systemRole, rootRole, adminRole int) []string {
	switch {
	case systemRole >= rootRole:
		return []string{BuiltInRoleRoot}
	case systemRole >= adminRole:
		return []string{BuiltInRoleAdmin}
	default:
		return nil
	}
}
