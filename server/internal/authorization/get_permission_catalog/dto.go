package get_permission_catalog

import "github.com/QuantumNous/new-api/internal/authorization/domain"

// Shared business metadata retain their existing public Go/JSON representation.
type ActionDefinition = domain.ActionDefinition
type ResourceDefinition = domain.ResourceDefinition
type RoleDescriptor = domain.RoleDescriptor
type Output struct {
	Resources []ResourceDefinition `json:"resources"`
	Roles     []RoleDescriptor     `json:"roles"`
}
