package admin_query_user_logs

type AdminLogQueryRequest struct {
	UserIDs        []int  `json:"user_ids"`
	Type           int    `json:"type"`
	StartTimestamp int64  `json:"start_timestamp"`
	EndTimestamp   int64  `json:"end_timestamp"`
	ModelName      string `json:"model_name"`
	Page           int    `json:"p"`
	PageSize       int    `json:"page_size"`
}
