package customoauth

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
)

type Store struct{}

func (Store) List() ([]domain.CustomOAuthProvider, error) {
	providers, err := model.GetAllCustomOAuthProviders()
	if err != nil {
		return nil, err
	}
	result := make([]domain.CustomOAuthProvider, len(providers))
	for i, provider := range providers {
		result[i] = providerFromRecord(provider)
	}
	return result, nil
}
func (Store) Get(id int) (domain.CustomOAuthProvider, error) {
	provider, err := model.GetCustomOAuthProviderById(id)
	if err != nil {
		return domain.CustomOAuthProvider{}, err
	}
	return providerFromRecord(provider), nil
}
func (Store) SlugTaken(slug string, excludeID int) bool { return model.IsSlugTaken(slug, excludeID) }
func (Store) Create(provider *domain.CustomOAuthProvider) error {
	record := providerToRecord(*provider)
	err := model.CreateCustomOAuthProvider(record)
	*provider = providerFromRecord(record)
	return err
}
func (Store) Update(provider *domain.CustomOAuthProvider) error {
	record := providerToRecord(*provider)
	err := model.UpdateCustomOAuthProvider(record)
	*provider = providerFromRecord(record)
	return err
}
func (Store) Delete(id int) error { return model.DeleteCustomOAuthProvider(id) }
func (Store) BindingCount(id int) (int64, error) {
	count, err := model.GetBindingCountByProviderId(id)
	if err != nil {
		common.SysError("Failed to get binding count for provider " + strconv.Itoa(id) + ": " + err.Error())
	}
	return count, err
}

type Registry struct{}

func (Registry) IsRegistered(slug string) bool { return oauth.IsProviderRegistered(slug) }
func (Registry) IsCustom(slug string) bool     { return oauth.IsCustomProvider(slug) }
func (Registry) Publish(provider domain.CustomOAuthProvider) {
	oauth.RegisterOrUpdateCustomProvider(providerToRecord(provider))
}
func (Registry) Unregister(slug string) { oauth.UnregisterCustomProvider(slug) }

func providerFromRecord(provider *model.CustomOAuthProvider) domain.CustomOAuthProvider {
	return domain.CustomOAuthProvider{
		Id:                    provider.Id,
		Name:                  provider.Name,
		Slug:                  provider.Slug,
		Icon:                  provider.Icon,
		Enabled:               provider.Enabled,
		ClientId:              provider.ClientId,
		AuthorizationEndpoint: provider.AuthorizationEndpoint,
		TokenEndpoint:         provider.TokenEndpoint,
		UserInfoEndpoint:      provider.UserInfoEndpoint,
		Scopes:                provider.Scopes,
		UserIdField:           provider.UserIdField,
		UsernameField:         provider.UsernameField,
		DisplayNameField:      provider.DisplayNameField,
		EmailField:            provider.EmailField,
		WellKnown:             provider.WellKnown,
		AuthStyle:             provider.AuthStyle,
		AccessPolicy:          provider.AccessPolicy,
		AccessDeniedMessage:   provider.AccessDeniedMessage,
		ClientSecret:          provider.ClientSecret,
		CreatedAt:             provider.CreatedAt,
		UpdatedAt:             provider.UpdatedAt,
	}
}
func providerToRecord(provider domain.CustomOAuthProvider) *model.CustomOAuthProvider {
	return &model.CustomOAuthProvider{
		Id:                    provider.Id,
		Name:                  provider.Name,
		Slug:                  provider.Slug,
		Icon:                  provider.Icon,
		Enabled:               provider.Enabled,
		ClientId:              provider.ClientId,
		AuthorizationEndpoint: provider.AuthorizationEndpoint,
		TokenEndpoint:         provider.TokenEndpoint,
		UserInfoEndpoint:      provider.UserInfoEndpoint,
		Scopes:                provider.Scopes,
		UserIdField:           provider.UserIdField,
		UsernameField:         provider.UsernameField,
		DisplayNameField:      provider.DisplayNameField,
		EmailField:            provider.EmailField,
		WellKnown:             provider.WellKnown,
		AuthStyle:             provider.AuthStyle,
		AccessPolicy:          provider.AccessPolicy,
		AccessDeniedMessage:   provider.AccessDeniedMessage,
		ClientSecret:          provider.ClientSecret,
		CreatedAt:             provider.CreatedAt,
		UpdatedAt:             provider.UpdatedAt,
	}
}
