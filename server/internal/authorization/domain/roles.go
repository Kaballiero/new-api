package domain

import "slices"

const (
	BuiltInRoleRoot  = "root"
	BuiltInRoleAdmin = "admin"
)

// RoleSpec describes a role. A superuser role is allowed every permission
// without an explicit policy entry.
type RoleSpec struct {
	Key         string
	Name        string
	Description string
	BuiltIn     bool
	Superuser   bool
	Sort        int
}

func BuiltInRoles() []RoleSpec {
	return []RoleSpec{
		{
			Key:         BuiltInRoleRoot,
			Name:        "Root",
			Description: "Built-in root authorization role",
			BuiltIn:     true,
			Superuser:   true,
			Sort:        0,
		},
		{
			Key:         BuiltInRoleAdmin,
			Name:        "Admin",
			Description: "Built-in admin authorization role",
			BuiltIn:     true,
			Superuser:   false,
			Sort:        10,
		},
	}
}

// RoleDescriptor exposes a role together with its baseline grant matrix.
type RoleDescriptor struct {
	Key       string         `json:"key"`
	Name      string         `json:"name"`
	BuiltIn   bool           `json:"built_in"`
	Superuser bool           `json:"superuser"`
	Grants    PermissionsMap `json:"grants"`
}

func (catalog Catalog) Roles() []RoleDescriptor {
	result := make([]RoleDescriptor, 0, len(catalog.BuiltInRoles))
	for _, spec := range catalog.BuiltInRoles {
		result = append(result, RoleDescriptor{
			Key:       spec.Key,
			Name:      spec.Name,
			BuiltIn:   spec.BuiltIn,
			Superuser: spec.Superuser,
			Grants:    catalog.RoleGrants(spec),
		})
	}
	return result
}
func (catalog Catalog) RoleGrants(spec RoleSpec) PermissionsMap {
	grants := make(PermissionsMap, len(catalog.Resources))
	for _, resource := range catalog.Resources {
		actions := make(map[string]bool, len(resource.Actions))
		for _, action := range resource.Actions {
			actions[action.Action] = spec.Superuser || slices.Contains(action.DefaultRoles, spec.Key)
		}
		grants[resource.Resource] = actions
	}
	return grants
}
func (catalog Catalog) RoleSpec(roleKey string) (RoleSpec, bool) {
	for _, spec := range catalog.BuiltInRoles {
		if spec.Key == roleKey {
			return spec, true
		}
	}
	return RoleSpec{}, false
}
func (catalog Catalog) IsSuperuserRole(roleKey string) bool {
	spec, ok := catalog.RoleSpec(roleKey)
	return ok && spec.Superuser
}
