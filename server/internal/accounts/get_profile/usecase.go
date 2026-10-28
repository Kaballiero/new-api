package get_profile

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	profiledto "github.com/QuantumNous/new-api/internal/accounts/dto"
)

// ProfileReader returns the safe profile data without storage details.
type ProfileReader interface {
	SelfProfile(id int) (domain.SelfProfile, error)
}

type Usecase struct {
	reader       ProfileReader
	capabilities func(int, int) any
	roles        domain.AdminRoles
	sidebar      func(string) string
}

func NewUsecase(reader ProfileReader, capabilities func(int, int) any, roles domain.AdminRoles, sidebar func(string) string) Usecase {
	return Usecase{reader: reader, capabilities: capabilities, roles: roles, sidebar: sidebar}
}

func (queries Usecase) Execute(input Input) (map[string]any, error) {
	id, authenticatedRole := input.ID, input.AuthenticatedRole
	profile, err := queries.reader.SelfProfile(id)
	if err != nil {
		return nil, err
	}
	response := profiledto.BuildSelfUserData(profile, queries.sidebar, queries.capabilities, queries.roles)
	// Permissions are read again for the role of the authenticated request.
	permissions := domain.DashboardPermissions(authenticatedRole, queries.roles)
	permissions["admin_permissions"] = queries.capabilities(id, authenticatedRole)
	response["permissions"] = permissions
	return response, nil
}
