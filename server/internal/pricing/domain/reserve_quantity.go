package domain

// ReserveQuantity names a measured quantity of an OpenRouter request that its
// reserve is priced by; the admin log records them by name as
// openrouter_reservation_quantities.
type ReserveQuantity string

const (
	// ReserveQuantitySearches is the web searches the request may run.
	ReserveQuantitySearches ReserveQuantity = "searches"
	// ReserveQuantityAudioSeconds is the seconds of a transcription file
	// measured from its audio headers.
	ReserveQuantityAudioSeconds ReserveQuantity = "audio_seconds"
)
