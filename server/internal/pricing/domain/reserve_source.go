package domain

// ReserveSource is what the reserve of an OpenRouter request is priced from;
// the admin log records it as openrouter_reservation_source.
type ReserveSource string

const (
	// ReserveSourceCatalog is the fresh upstream catalog.
	ReserveSourceCatalog ReserveSource = "catalog"
	// ReserveSourceCatalogStale is a stale catalog document taken because the
	// live endpoints failed.
	ReserveSourceCatalogStale ReserveSource = "catalog_stale"
	// ReserveSourceLive is the endpoints OpenRouter publishes at request time.
	ReserveSourceLive ReserveSource = "live"
	// ReserveSourceConfiguredPrice is the configured local price of the channel
	// model.
	ReserveSourceConfiguredPrice ReserveSource = "configured_price"
	// ReserveSourceManual is the approximate reserve of the channel model.
	ReserveSourceManual ReserveSource = "manual"
)
