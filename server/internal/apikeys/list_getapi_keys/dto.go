package list_getapi_keys

// Input selects exactly one mode. The user mode sets UserID and optionally
// AfterID and Limit; the ids mode sets IDs. Nil means the parameter is absent.
type Input struct {
	UserID  *int
	AfterID *int
	Limit   *int
	IDs     []int
}

// Key is an API key without its full value.
type Key struct {
	ID             int    `json:"id"`
	UserID         int    `json:"user_id"`
	Name           string `json:"name"`
	MaskedKey      string `json:"masked_key"`
	Status         int    `json:"status"`
	CreatedTime    int64  `json:"created_time"`
	AccessedTime   int64  `json:"accessed_time"`
	ExpiredTime    int64  `json:"expired_time"`
	RemainQuota    int    `json:"remain_quota"`
	UsedQuota      int    `json:"used_quota"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
}

// Output is the user mode page. NextAfterID is the after_id of the next page,
// nil on the last one; Total counts all keys of the user that are not deleted.
type Output struct {
	Items       []Key `json:"items"`
	NextAfterID *int  `json:"next_after_id"`
	Total       int64 `json:"total"`
}

// Batch is the ids mode response.
type Batch struct {
	Items []Key `json:"items"`
}
