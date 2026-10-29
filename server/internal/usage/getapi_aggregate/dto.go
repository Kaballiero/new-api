package getapi_aggregate

type GetAPIUsageAggregateRequest struct {
	UserIDs        []int   `json:"user_ids"`
	TokenID        *int    `json:"token_id,omitempty"`
	StartTimestamp *int64  `json:"start_timestamp"`
	EndTimestamp   *int64  `json:"end_timestamp"`
	GroupBy        string  `json:"group_by"`
	Granularity    *string `json:"granularity,omitempty"`
}

type GetAPIUsageAggregateRow struct {
	UserID       int     `json:"user_id"`
	ModelName    *string `json:"model_name,omitempty"`
	TokenID      *int    `json:"token_id,omitempty"`
	TokenName    *string `json:"token_name,omitempty"`
	TokenDeleted *bool   `json:"token_deleted,omitempty"`
	CreatedAt    *int64  `json:"created_at,omitempty" gorm:"column:bucket_start"`
	Quota        int64   `json:"quota"`
	Count        int64   `json:"count"`
	TokenUsed    int64   `json:"token_used"`
}

type GetAPIUsageRecent struct {
	UserID int   `json:"user_id"`
	RPM    int64 `json:"rpm"`
	TPM    int64 `json:"tpm"`
}

type GetAPIUsageAggregateResult struct {
	Rows   []GetAPIUsageAggregateRow `json:"rows"`
	Recent []GetAPIUsageRecent       `json:"recent,omitempty"`
}
