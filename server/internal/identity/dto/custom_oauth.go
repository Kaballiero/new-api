package dto

import "github.com/QuantumNous/new-api/internal/identity/domain"

type CustomOAuthProviderResponse struct {
	Id                    int    `json:"id"`
	Name                  string `json:"name"`
	Slug                  string `json:"slug"`
	Icon                  string `json:"icon"`
	Enabled               bool   `json:"enabled"`
	ClientId              string `json:"client_id"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"user_info_endpoint"`
	Scopes                string `json:"scopes"`
	UserIdField           string `json:"user_id_field"`
	UsernameField         string `json:"username_field"`
	DisplayNameField      string `json:"display_name_field"`
	EmailField            string `json:"email_field"`
	WellKnown             string `json:"well_known"`
	AuthStyle             int    `json:"auth_style"`
	AccessPolicy          string `json:"access_policy"`
	AccessDeniedMessage   string `json:"access_denied_message"`
}

func CustomOAuthProviderResponseFrom(provider domain.CustomOAuthProvider) *CustomOAuthProviderResponse {
	return &CustomOAuthProviderResponse{
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
	}
}
