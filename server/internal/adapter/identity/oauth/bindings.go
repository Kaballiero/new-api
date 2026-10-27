package oauthadapter

import (
	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"github.com/QuantumNous/new-api/internal/identity/get_oauth_bindings"
)

type Bindings struct {
	Accounts accountstore.Store

	SQL identitystore.Store

	Role           func() int
	ReadProviderID func() (int, error)
}

func (op Bindings) Bindings(userID int) ([]get_oauth_bindings.Binding, error) {
	records, err := op.SQL.GetUserOAuthBindingsByUserId(userID)
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
func (op Bindings) Provider(id int) (get_oauth_bindings.Provider, error) {
	provider, err := op.SQL.GetCustomOAuthProviderById(id)
	if err != nil {
		return get_oauth_bindings.Provider{}, err
	}
	return get_oauth_bindings.Provider{Name: provider.Name, Slug: provider.Slug, Icon: provider.Icon}, nil
}
func (op Bindings) TargetRole(id int) (int, error) {
	user, err := op.Accounts.GetUserById(id, false)
	if err != nil {
		return 0, err
	}
	return user.Role, nil
}
func (op Bindings) ActorRole() int           { return op.Role() }
func (Bindings) RootRole() int               { return common.RoleRootUser }
func (op Bindings) ProviderID() (int, error) { return op.ReadProviderID() }
func (op Bindings) DeleteBinding(userID, providerID int) error {
	return op.SQL.DeleteUserOAuthBinding(userID, providerID)
}
