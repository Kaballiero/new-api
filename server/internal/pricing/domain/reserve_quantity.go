package domain

// ReserveQuantity names a measured quantity of an OpenRouter request that its
// reserve is priced by; the admin log records them by name as
// openrouter_reservation_quantities.
type ReserveQuantity string

const (
	// ReserveQuantitySearches is the web searches the request may run.
	ReserveQuantitySearches ReserveQuantity = "searches"
	// ReserveQuantityPromptTokens is the prompt tokens the state and the
	// questions of a decisions request count as, or the input tokens of a
	// speech or rerank request.
	ReserveQuantityPromptTokens ReserveQuantity = "prompt_tokens"
	// ReserveQuantityAudioSeconds is the seconds of a transcription file
	// measured from its audio headers.
	ReserveQuantityAudioSeconds ReserveQuantity = "audio_seconds"
	// ReserveQuantityCharacters is the characters of a speech input.
	ReserveQuantityCharacters ReserveQuantity = "characters"
	// ReserveQuantityUTF8Bytes is the UTF-8 bytes of a speech input.
	ReserveQuantityUTF8Bytes ReserveQuantity = "utf8_bytes"
	// ReserveQuantitySpeechSeconds is the seconds of the speech output,
	// approximated from its input.
	ReserveQuantitySpeechSeconds ReserveQuantity = "speech_seconds"
	// ReserveQuantitySearchUnits is the search units of a rerank request.
	ReserveQuantitySearchUnits ReserveQuantity = "search_units"
)
