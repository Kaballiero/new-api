package domain

import "sort"

type OverridePolicy struct{ Resource, Action, Effect string }

func UserOverridePolicies(e PolicyReader, catalog Catalog, resource string, actions map[string]bool) []OverridePolicy {
	overrides := make([]OverridePolicy, 0, len(actions))
	for _, action := range catalog.Actions(resource) {
		desired, ok := actions[action.Action]
		if !ok {
			continue
		}
		permission := Permission{Resource: resource, Action: action.Action}
		if desired == RoleBaselineAllows(e, BuiltInRoleAdmin, permission) {
			continue
		}
		effect := EffectDeny
		if desired {
			effect = EffectAllow
		}
		overrides = append(overrides, OverridePolicy{
			Resource: resource,
			Action:   action.Action,
			Effect:   effect,
		})
	}
	sort.Slice(overrides, func(i, j int) bool {
		return overrides[i].Action < overrides[j].Action
	})
	return overrides
}
