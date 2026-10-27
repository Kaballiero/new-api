package create_custom_oauth_provider

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Store interface {
	SlugTaken(string, int) bool
	Create(*domain.CustomOAuthProvider) error
}
type Registry interface {
	IsRegistered(string) bool
	IsCustom(string) bool
	Publish(domain.CustomOAuthProvider)
}
type Usecase struct {
	store    Store
	registry Registry
}

func NewUsecase(store Store, registry Registry) Usecase {
	return Usecase{store: store, registry: registry}
}
func (u Usecase) Execute(req CreateCustomOAuthProviderRequest) (domain.CustomOAuthProvider, error) {
	// Check if slug is already taken
	if u.store.SlugTaken(req.Slug, 0) {
		return domain.CustomOAuthProvider{}, domain.ErrCustomOAuthSlugTaken
	}

	// Check if slug conflicts with built-in providers
	if u.registry.IsRegistered(req.Slug) && !u.registry.IsCustom(req.Slug) {
		return domain.CustomOAuthProvider{}, domain.ErrCustomOAuthBuiltinConflict
	}

	provider := &domain.CustomOAuthProvider{
		Name:                  req.Name,
		Slug:                  req.Slug,
		Icon:                  req.Icon,
		Enabled:               req.Enabled,
		ClientId:              req.ClientId,
		ClientSecret:          req.ClientSecret,
		AuthorizationEndpoint: req.AuthorizationEndpoint,
		TokenEndpoint:         req.TokenEndpoint,
		UserInfoEndpoint:      req.UserInfoEndpoint,
		Scopes:                req.Scopes,
		UserIdField:           req.UserIdField,
		UsernameField:         req.UsernameField,
		DisplayNameField:      req.DisplayNameField,
		EmailField:            req.EmailField,
		WellKnown:             req.WellKnown,
		AuthStyle:             req.AuthStyle,
		AccessPolicy:          req.AccessPolicy,
		AccessDeniedMessage:   req.AccessDeniedMessage,
	}

	if err := u.store.Create(provider); err != nil {
		return domain.CustomOAuthProvider{}, err
	}

	// Register the provider in the OAuth registry
	u.registry.Publish(*provider)

	return *provider, nil
}
