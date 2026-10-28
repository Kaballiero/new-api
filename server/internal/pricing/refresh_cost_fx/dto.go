package refresh_cost_fx

type FXProviderResponse struct {
	Date   string
	Valute map[string]Currency
}
type Currency struct {
	CharCode string
	Value    float64
	Nominal  float64
}

type ModelCostFXTaskResult struct {
	Source      string `json:"source"`
	Version     int64  `json:"version"`
	EffectiveAt int64  `json:"effective_at"`
	FetchedAt   int64  `json:"fetched_at"`
}
