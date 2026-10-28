package get_billing_usage

type Output struct {
	Object     string  `json:"object"`
	TotalUsage float64 `json:"total_usage"`
}
