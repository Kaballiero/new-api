package get_catalog_freshness

// Output is the freshness of the published upstream catalog. FetchedAt is 0
// without a published catalog.
type Output struct {
	FetchedAt   int64 `json:"fetched_at"`
	MaxAgeHours int   `json:"max_age_hours"`
	Stale       bool  `json:"stale"`
}
