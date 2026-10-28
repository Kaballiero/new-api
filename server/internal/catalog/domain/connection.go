package domain

type ModelConnection struct {
	Group       string  `json:"group"`
	Model       string  `json:"model"`
	ChannelId   int     `json:"channel_id"`
	Enabled     bool    `json:"enabled"`
	Priority    *int64  `json:"priority"`
	Weight      uint    `json:"weight"`
	Tag         *string `json:"tag"`
	ChannelType int     `json:"channel_type"`
	ChannelName string  `json:"channel_name"`
}
