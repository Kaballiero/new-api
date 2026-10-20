package get_profile

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	profiledto "github.com/QuantumNous/new-api/internal/accounts/dto"
)

// ProfileReader возвращает безопасные данные профиля без сведений о хранении.
type ProfileReader interface {
	SelfProfile(id int) (domain.SelfProfile, error)
}

type Usecase struct {
	Reader       ProfileReader
	Capabilities func(int, int) any
}

func NewUsecase(reader ProfileReader, capabilities func(int, int) any) Usecase {
	return Usecase{Reader: reader, Capabilities: capabilities}
}

func (queries Usecase) Execute(input Input) (map[string]any, error) {
	id, authenticatedRole := input.ID, input.AuthenticatedRole
	profile, err := queries.Reader.SelfProfile(id)
	if err != nil {
		return nil, err
	}
	response := profiledto.BuildSelfUserData(profile, queries.Capabilities)
	// Сохраняем повторное чтение прав по роли аутентифицированного запроса.
	permissions := domain.DashboardPermissions(authenticatedRole)
	permissions["admin_permissions"] = queries.Capabilities(id, authenticatedRole)
	response["permissions"] = permissions
	return response, nil
}
