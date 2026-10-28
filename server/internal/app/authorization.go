package app

import (
	authz "github.com/QuantumNous/new-api/internal/app/authorizationcore"
	"github.com/QuantumNous/new-api/internal/authorization/get_permission_catalog"
)

func NewAuthorizationHTTP() get_permission_catalog.HTTP {
	return get_permission_catalog.NewHTTP(authz.Catalog, authz.Roles)
}
