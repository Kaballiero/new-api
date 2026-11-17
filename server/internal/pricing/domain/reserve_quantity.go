package domain

// ReserveQuantity names a measured quantity of an OpenRouter request that its
// reserve is priced by; the admin log records them by name as
// openrouter_reservation_quantities.
type ReserveQuantity string

// ReserveQuantitySearches is the web searches the request may run.
const ReserveQuantitySearches ReserveQuantity = "searches"
