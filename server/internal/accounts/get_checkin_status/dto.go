package get_checkin_status

type Response struct {
	Success bool  `json:"success"`
	Data    *Data `json:"data,omitempty"`
}
type Data struct {
	Enabled  bool           `json:"enabled"`
	MinQuota int            `json:"min_quota"`
	MaxQuota int            `json:"max_quota"`
	Stats    map[string]any `json:"stats"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
