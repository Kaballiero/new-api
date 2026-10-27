package update_custom_oauth_provider

type UpdateCustomOAuthProviderRequest struct {
	Name                  string  `json:"name"`
	Slug                  string  `json:"slug"`
	Icon                  *string `json:"icon"`    // Optional: if nil, keep existing
	Enabled               *bool   `json:"enabled"` // Optional: if nil, keep existing
	ClientId              string  `json:"client_id"`
	ClientSecret          string  `json:"client_secret"` // Optional: if empty, keep existing
	AuthorizationEndpoint string  `json:"authorization_endpoint"`
	TokenEndpoint         string  `json:"token_endpoint"`
	UserInfoEndpoint      string  `json:"user_info_endpoint"`
	Scopes                string  `json:"scopes"`
	UserIdField           string  `json:"user_id_field"`
	UsernameField         string  `json:"username_field"`
	DisplayNameField      string  `json:"display_name_field"`
	EmailField            string  `json:"email_field"`
	WellKnown             *string `json:"well_known"`            // Optional: if nil, keep existing
	AuthStyle             *int    `json:"auth_style"`            // Optional: if nil, keep existing
	AccessPolicy          *string `json:"access_policy"`         // Optional: if nil, keep existing
	AccessDeniedMessage   *string `json:"access_denied_message"` // Optional: if nil, keep existing
}
