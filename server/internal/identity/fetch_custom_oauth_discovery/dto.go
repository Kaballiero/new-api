package fetch_custom_oauth_discovery

type FetchCustomOAuthDiscoveryRequest struct {
	WellKnownURL string `json:"well_known_url"`
	IssuerURL    string `json:"issuer_url"`
}

type Response struct {
	WellKnownURL string         `json:"well_known_url"`
	Discovery    map[string]any `json:"discovery"`
}
