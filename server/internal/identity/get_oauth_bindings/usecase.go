package get_oauth_bindings

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
)

type Reader interface {
	Bindings(userID int) ([]Binding, error)
	Provider(providerID int) (Provider, error)
}
type AdminReader interface {
	Reader
	TargetRole(userID int) (int, error)
	ActorRole() int
	RootRole() int
}

func (usecase Usecase) Execute(userID int) ([]UserOAuthBindingResponse, error) {
	bindings, err := usecase.reader.Bindings(userID)
	if err != nil {
		return nil, err
	}
	response := make([]UserOAuthBindingResponse, 0, len(bindings))
	for _, binding := range bindings {
		provider, err := usecase.reader.Provider(binding.ProviderID)
		if err != nil {
			continue
		}
		response = append(response, UserOAuthBindingResponse{ProviderId: binding.ProviderID, ProviderName: provider.Name, ProviderSlug: provider.Slug, ProviderIcon: provider.Icon, ProviderUserId: binding.ProviderUserID})
	}
	return response, nil
}

// ReadAsAdmin returns the bindings of a user whose role the actor can manage.
func ReadAsAdmin(userID int, reader AdminReader) ([]UserOAuthBindingResponse, error) {
	role, err := reader.TargetRole(userID)
	if err != nil {
		return nil, err
	}
	if !domain.CanManageTargetRole(reader.ActorRole(), role, reader.RootRole()) {
		return nil, identitydomain.ErrTargetForbidden
	}
	return NewUsecase(reader).Execute(userID)
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}
