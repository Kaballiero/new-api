package domain

type BillingFXBasis struct {
	USDRate            float64 `json:"usd_rate"`
	Factor             float64 `json:"factor"`
	Source             string  `json:"source"`
	PublicationVersion int64   `json:"publication_version"`
	EffectiveAt        int64   `json:"effective_at"`
	FetchedAt          int64   `json:"fetched_at"`
}
