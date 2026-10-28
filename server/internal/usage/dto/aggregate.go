package dto

type AggregateScopeRequest struct {
	UserIDs        []int  `json:"user_ids"`
	StartTimestamp *int64 `json:"start_timestamp"`
	EndTimestamp   *int64 `json:"end_timestamp"`
}
