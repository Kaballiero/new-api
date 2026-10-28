package get_oauth_bindings

type Binding struct {
	ProviderID     int
	ProviderUserID string
}
type Provider struct{ Name, Slug, Icon string }
type UserOAuthBindingResponse struct {
	ProviderId     int    `json:"provider_id"`
	ProviderName   string `json:"provider_name"`
	ProviderSlug   string `json:"provider_slug"`
	ProviderIcon   string `json:"provider_icon"`
	ProviderUserId string `json:"provider_user_id"`
}
