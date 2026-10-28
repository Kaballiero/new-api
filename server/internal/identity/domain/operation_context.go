package domain

type ChannelKeyReadContext struct {
	ChannelID int `json:"channel_id"`
}
type AccountBindingContext struct {
	Provider string `json:"provider"`
	Email    string `json:"email,omitempty"`
	Code     string `json:"code,omitempty"`
}
type AccountUnbindingContext struct {
	ProviderID int `json:"provider_id"`
}
