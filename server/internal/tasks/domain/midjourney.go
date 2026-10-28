package domain

type Midjourney struct {
	Id          int    `json:"id"`
	Code        int    `json:"code"`
	UserId      int    `json:"user_id"`
	Action      string `json:"action"`
	MjId        string `json:"mj_id"`
	Prompt      string `json:"prompt"`
	PromptEn    string `json:"prompt_en"`
	Description string `json:"description"`
	State       string `json:"state"`
	SubmitTime  int64  `json:"submit_time"`
	StartTime   int64  `json:"start_time"`
	FinishTime  int64  `json:"finish_time"`
	ImageUrl    string `json:"image_url"`
	VideoUrl    string `json:"video_url"`
	VideoUrls   string `json:"video_urls"`
	Status      string `json:"status"`
	Progress    string `json:"progress"`
	FailReason  string `json:"fail_reason"`
	ChannelId   int    `json:"channel_id"`
	Quota       int    `json:"quota"`
	Buttons     string `json:"buttons"`
	Properties  string `json:"properties"`

	TokenId          int `json:"-"`
	BillingChannelId int `json:"-"`
}

// GetBillingChannelId preserves the historical fallback used by refunds.
func (task *Midjourney) GetBillingChannelId() int {
	if task.BillingChannelId > 0 {
		return task.BillingChannelId
	}
	return task.ChannelId
}
