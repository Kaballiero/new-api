package get_key_status

type CreditSummary struct {
	Object         string `json:"object"`
	TotalGranted   int    `json:"total_granted"`
	TotalUsed      int    `json:"total_used"`
	TotalAvailable int    `json:"total_available"`
	ExpiresAt      int64  `json:"expires_at"`
}
