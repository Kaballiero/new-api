package aggregate_logs

type Response struct {
	Quota        int64 `json:"quota"`
	RefundQuota  int64 `json:"refund_quota"`
	RequestCount int64 `json:"request_count"`
}
