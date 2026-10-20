package domain

import "slices"

// ActionDefinition describes a single action exposed by a resource. DefaultRoles
// lists the role keys that receive this action as part of their baseline grants.
type ActionDefinition struct {
	Action         string   `json:"action"`
	LabelKey       string   `json:"label_key"`
	DescriptionKey string   `json:"description_key"`
	DefaultRoles   []string `json:"-"`
}

// ResourceDefinition describes a resource and the actions it exposes.
type ResourceDefinition struct {
	Resource string             `json:"resource"`
	LabelKey string             `json:"label_key"`
	Actions  []ActionDefinition `json:"actions"`
}

type Catalog struct {
	Resources    []ResourceDefinition
	BuiltInRoles []RoleSpec
}

func (catalog Catalog) ResourcesCopy() []ResourceDefinition {
	result := make([]ResourceDefinition, 0, len(catalog.Resources))
	for _, resource := range catalog.Resources {
		result = append(result, ResourceDefinition{
			Resource: resource.Resource,
			LabelKey: resource.LabelKey,
			Actions:  append([]ActionDefinition(nil), resource.Actions...),
		})
	}
	return result
}

// AllPermissions returns every registered permission.
func (catalog Catalog) AllPermissions() []Permission {
	permissions := make([]Permission, 0)
	for _, resource := range catalog.Resources {
		for _, action := range resource.Actions {
			permissions = append(permissions, Permission{
				Resource: resource.Resource,
				Action:   action.Action,
			})
		}
	}
	return permissions
}

// PermissionsForRole returns the permissions whose DefaultRoles include roleKey.
func (catalog Catalog) PermissionsForRole(roleKey string) []Permission {
	permissions := make([]Permission, 0)
	for _, resource := range catalog.Resources {
		for _, action := range resource.Actions {
			if slices.Contains(action.DefaultRoles, roleKey) {
				permissions = append(permissions, Permission{
					Resource: resource.Resource,
					Action:   action.Action,
				})
			}
		}
	}
	return permissions
}
func (catalog Catalog) HasResource(resource string) bool {
	for _, known := range catalog.Resources {
		if known.Resource == resource {
			return true
		}
	}
	return false
}
func (catalog Catalog) Actions(resource string) []ActionDefinition {
	for _, known := range catalog.Resources {
		if known.Resource == resource {
			return known.Actions
		}
	}
	return nil
}
func (catalog Catalog) HasPermission(permission Permission) bool {
	for _, resource := range catalog.Resources {
		if resource.Resource != permission.Resource {
			continue
		}
		for _, action := range resource.Actions {
			if action.Action == permission.Action {
				return true
			}
		}
	}
	return false
}
