package domain

type UnbillableModel struct {
	ModelName   string `json:"model_name"`
	CatalogType string `json:"catalog_type,omitempty"`
	Reason      string `json:"reason"`
	// SuggestedReserveUSD is the most common OpenRouter reserve among billable models of the same catalog type.
	SuggestedReserveUSD *float64 `json:"suggested_reserve_usd,omitempty"`
}
