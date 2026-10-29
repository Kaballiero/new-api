package getapi_logs

type GetAPIUsageLogsRequest struct {
	UserID         int     `json:"user_id"`
	TokenID        *int    `json:"token_id,omitempty"`
	StartTimestamp *int64  `json:"start_timestamp"`
	EndTimestamp   *int64  `json:"end_timestamp"`
	ModelName      *string `json:"model_name,omitempty"`
	Types          []int   `json:"types,omitempty"`
	Limit          int     `json:"limit"`
	BeforeID       *int    `json:"before_id,omitempty"`
	AfterID        *int    `json:"after_id,omitempty"`
	WithTotal      bool    `json:"with_total,omitempty"`
}

type GetAPIUsageLog struct {
	ID                int    `json:"id"`
	CreatedAt         int64  `json:"created_at"`
	Type              int    `json:"type"`
	Content           string `json:"content"`
	Username          string `json:"username"`
	TokenName         string `json:"token_name"`
	TokenID           int    `json:"token_id"`
	ModelName         string `json:"model_name"`
	Quota             int    `json:"quota"`
	PromptTokens      int    `json:"prompt_tokens"`
	CompletionTokens  int    `json:"completion_tokens"`
	UseTime           int    `json:"use_time"`
	IsStream          bool   `json:"is_stream"`
	Group             string `json:"group"`
	RequestID         string `json:"request_id"`
	UpstreamRequestID string `json:"upstream_request_id"`
	Other             string `json:"other"`
	UserID            int    `json:"user_id"`
}

type GetAPIUsageLogsResult struct {
	Items   []GetAPIUsageLog `json:"items"`
	HasMore bool             `json:"has_more"`
	Total   *int64           `json:"total,omitempty"`
}
