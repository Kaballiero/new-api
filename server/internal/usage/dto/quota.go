package dto

type QuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	ModelName string `json:"model_name"`
	CreatedAt int64  `json:"created_at"`
	UseGroup  string `json:"use_group"`
	TokenID   int    `json:"token_id"`
	ChannelID int    `json:"channel_id"`
	NodeName  string `json:"node_name"`
	TokenUsed int    `json:"token_used"`
	Count     int    `json:"count"`
	Quota     int    `json:"quota"`
}

type BucketedQuotaData struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	ModelName string `json:"model_name"`
	CreatedAt int64  `json:"created_at"`
	TokenUsed int64  `json:"token_used"`
	Count     int64  `json:"count"`
	Quota     int64  `json:"quota"`
}

type BucketedUserQuotaData struct {
	Username  string `json:"username"`
	CreatedAt int64  `json:"created_at"`
	TokenUsed int64  `json:"token_used"`
	Count     int64  `json:"count"`
	Quota     int64  `json:"quota"`
}

type FlowQuotaData struct {
	UserID      int    `json:"user_id,omitempty"`
	Username    string `json:"username,omitempty"`
	NodeName    string `json:"node_name,omitempty"`
	TokenID     int    `json:"token_id,omitempty"`
	TokenName   string `json:"token_name,omitempty"`
	UseGroup    string `json:"use_group"`
	ChannelID   int    `json:"channel_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
	ModelName   string `json:"model_name"`
	TokenUsed   int    `json:"token_used"`
	Count       int    `json:"count"`
	Quota       int    `json:"quota"`
}
