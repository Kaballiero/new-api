package domain

// ReserveQuantity names a measured quantity of an OpenRouter request that its
// reserve is priced by; the admin log records them by name as
// openrouter_reservation_quantities.
type ReserveQuantity string

const (
	// ReserveQuantitySearches is the web searches the request may run.
	ReserveQuantitySearches ReserveQuantity = "searches"
	// ReserveQuantityPromptTokens is the prompt tokens the state and the
	// questions of a decisions request count as.
	ReserveQuantityPromptTokens ReserveQuantity = "prompt_tokens"
)
