package oauthadapter

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/get_oauth_bindings"
	"github.com/QuantumNous/new-api/model"
)

type Bindings struct {
	Role           func() int
	ReadProviderID func() (int, error)
}

func (Bindings) Bindings(userID int) ([]get_oauth_bindings.Binding, error) {
	records, err := model.GetUserOAuthBindingsByUserId(userID)
	if err != nil {
		return nil, err
	}
	var result []get_oauth_bindings.Binding
	if records != nil {
		result = make([]get_oauth_bindings.Binding, len(records))
		for i, record := range records {
			result[i] = get_oauth_bindings.Binding{ProviderID: record.ProviderId, ProviderUserID: record.ProviderUserId}
		}
	}
	return result, nil
}
func (Bindings) Provider(id int) (get_oauth_bindings.Provider, error) {
	provider, err := model.GetCustomOAuthProviderById(id)
	if err != nil {
		return get_oauth_bindings.Provider{}, err
	}
	return get_oauth_bindings.Provider{Name: provider.Name, Slug: provider.Slug, Icon: provider.Icon}, nil
}
func (Bindings) TargetRole(id int) (int, error) {
	user, err := model.GetUserById(id, false)
	if err != nil {
		return 0, err
	}
	return user.Role, nil
}
func (op Bindings) ActorRole() int           { return op.Role() }
func (Bindings) RootRole() int               { return common.RoleRootUser }
func (op Bindings) ProviderID() (int, error) { return op.ReadProviderID() }
func (Bindings) DeleteBinding(userID, providerID int) error {
	return model.DeleteUserOAuthBinding(userID, providerID)
}
