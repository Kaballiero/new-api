package check_in

type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    *Data  `json:"data,omitempty"`
}
type Data struct {
	QuotaAwarded int    `json:"quota_awarded"`
	CheckinDate  string `json:"checkin_date"`
}
