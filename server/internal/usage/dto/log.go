package dto

type Log struct {
	Id                int    `json:"id"`
	UserId            int    `json:"user_id"`
	CreatedAt         int64  `json:"created_at"`
	Type              int    `json:"type"`
	Content           string `json:"content"`
	Username          string `json:"username"`
	TokenName         string `json:"token_name"`
	ModelName         string `json:"model_name"`
	Quota             int    `json:"quota"`
	PromptTokens      int    `json:"prompt_tokens"`
	CompletionTokens  int    `json:"completion_tokens"`
	UseTime           int    `json:"use_time"`
	IsStream          bool   `json:"is_stream"`
	ChannelId         int    `json:"channel"`
	ChannelName       string `json:"channel_name"`
	TokenId           int    `json:"token_id"`
	Group             string `json:"group"`
	Ip                string `json:"ip"`
	RequestId         string `json:"request_id,omitempty"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty"`
	Other             string `json:"other"`
}
