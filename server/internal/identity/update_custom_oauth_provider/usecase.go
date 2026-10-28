package update_custom_oauth_provider

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Input struct {
	ID      int
	Request UpdateCustomOAuthProviderRequest
}
type Store interface {
	Get(int) (domain.CustomOAuthProvider, error)
	SlugTaken(string, int) bool
	Update(*domain.CustomOAuthProvider) error
}
type Registry interface {
	IsRegistered(string) bool
	IsCustom(string) bool
	Publish(domain.CustomOAuthProvider)
	Unregister(string)
}
type Usecase struct {
	store    Store
	registry Registry
}

func NewUsecase(store Store, registry Registry) Usecase {
	return Usecase{store: store, registry: registry}
}
func (u Usecase) Execute(input Input) (domain.CustomOAuthProvider, error) {
	// Get existing provider
	provider, err := u.store.Get(input.ID)
	if err != nil {
		return domain.CustomOAuthProvider{}, domain.ErrCustomOAuthProviderMissing
	}

	oldSlug := provider.Slug
	req := input.Request

	// Check if new slug is taken by another provider
	if req.Slug != "" && req.Slug != provider.Slug {
		if u.store.SlugTaken(req.Slug, input.ID) {
			return domain.CustomOAuthProvider{}, domain.ErrCustomOAuthSlugTaken
		}
		// Check if slug conflicts with built-in providers
		if u.registry.IsRegistered(req.Slug) && !u.registry.IsCustom(req.Slug) {
			return domain.CustomOAuthProvider{}, domain.ErrCustomOAuthBuiltinConflict
		}
	}

	// Update fields
	if req.Name != "" {
		provider.Name = req.Name
	}
	if req.Slug != "" {
		provider.Slug = req.Slug
	}
	if req.Icon != nil {
		provider.Icon = *req.Icon
	}
	if req.Enabled != nil {
		provider.Enabled = *req.Enabled
	}
	if req.ClientId != "" {
		provider.ClientId = req.ClientId
	}
	if req.ClientSecret != "" {
		provider.ClientSecret = req.ClientSecret
	}
	if req.AuthorizationEndpoint != "" {
		provider.AuthorizationEndpoint = req.AuthorizationEndpoint
	}
	if req.TokenEndpoint != "" {
		provider.TokenEndpoint = req.TokenEndpoint
	}
	if req.UserInfoEndpoint != "" {
		provider.UserInfoEndpoint = req.UserInfoEndpoint
	}
	if req.Scopes != "" {
		provider.Scopes = req.Scopes
	}
	if req.UserIdField != "" {
		provider.UserIdField = req.UserIdField
	}
	if req.UsernameField != "" {
		provider.UsernameField = req.UsernameField
	}
	if req.DisplayNameField != "" {
		provider.DisplayNameField = req.DisplayNameField
	}
	if req.EmailField != "" {
		provider.EmailField = req.EmailField
	}
	if req.WellKnown != nil {
		provider.WellKnown = *req.WellKnown
	}
	if req.AuthStyle != nil {
		provider.AuthStyle = *req.AuthStyle
	}
	if req.AccessPolicy != nil {
		provider.AccessPolicy = *req.AccessPolicy
	}
	if req.AccessDeniedMessage != nil {
		provider.AccessDeniedMessage = *req.AccessDeniedMessage
	}

	if err := u.store.Update(&provider); err != nil {
		return domain.CustomOAuthProvider{}, err
	}

	// Update the provider in the OAuth registry
	if oldSlug != provider.Slug {
		u.registry.Unregister(oldSlug)
	}
	u.registry.Publish(provider)

	return provider, nil
}
