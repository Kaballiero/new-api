package domain

import "slices"

type PolicyReader interface {
	GetFilteredPolicy(int, ...string) ([][]string, error)
}
type AccessRules struct {
	Catalog      func() Catalog
	ResolveRoles func(int, int) []string
	Policy       func() PolicyReader
}

func (rules AccessRules) Allowed(userID int, systemRole int, permission Permission) bool {
	roles := rules.ResolveRoles(userID, systemRole)
	if len(roles) == 0 {
		return false
	}
	if slices.ContainsFunc(roles, func(role string) bool { return rules.Catalog().IsSuperuserRole(role) }) {
		return true
	}
	if !rules.Catalog().HasPermission(permission) {
		return false
	}

	e := rules.Policy()
	if e == nil {
		return false
	}
	if effect, ok := ExplicitSubjectEffect(e, UserSubject(userID), permission); ok {
		return effect == EffectAllow
	}
	for _, role := range roles {
		if RoleBaselineAllows(e, role, permission) {
			return true
		}
	}
	return false
}

func RoleBaselineAllows(e PolicyReader, roleKey string, permission Permission) bool {
	effect, ok := ExplicitSubjectEffect(e, RoleSubject(roleKey), permission)
	return ok && effect == EffectAllow
}
func ExplicitSubjectEffect(e PolicyReader, subject string, permission Permission) (string, bool) {
	policies, err := e.GetFilteredPolicy(0, subject, permission.Resource, permission.Action)
	if err != nil {
		return "", false
	}
	hasAllow := false
	for _, policy := range policies {
		switch PolicyEffect(policy) {
		case EffectDeny:
			return EffectDeny, true
		case EffectAllow:
			hasAllow = true
		}
	}
	if hasAllow {
		return EffectAllow, true
	}
	return "", false
}
func PolicyEffect(policy []string) string {
	if len(policy) < 4 || policy[3] == "" {
		return EffectAllow
	}
	return policy[3]
}
