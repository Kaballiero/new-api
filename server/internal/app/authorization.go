package app

import (
	"github.com/QuantumNous/new-api/internal/authorization/get_permission_catalog"
	"github.com/QuantumNous/new-api/service/authz"
)

func NewAuthorizationHTTP() get_permission_catalog.HTTP {
	return get_permission_catalog.NewHTTP(authz.Catalog, authz.Roles)
}
