package aggregate_usage_data

import "github.com/QuantumNous/new-api/internal/usage/dto"

type Request struct {
	dto.AggregateScopeRequest
	GroupBy     string `json:"group_by"`
	Granularity string `json:"granularity"`
}

type Row struct {
	UserID    *int    `json:"user_id,omitempty"`
	ModelName *string `json:"model_name,omitempty"`
	CreatedAt *int64  `json:"created_at,omitempty"`
	Quota     int64   `json:"quota"`
	Count     int64   `json:"count"`
	TokenUsed int64   `json:"token_used"`
}
