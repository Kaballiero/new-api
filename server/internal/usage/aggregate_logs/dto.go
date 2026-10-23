package aggregate_logs

type Response struct {
	Quota        int64 `json:"quota"`
	RequestCount int64 `json:"request_count"`
}
