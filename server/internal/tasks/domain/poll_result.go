package domain

import "encoding/json"

type TaskInfo struct {
	UpstreamCostUSD       *float64
	UpstreamCostConfirmed bool
	Code                  int
	TaskID                string
	Status                string
	Reason                string
	Url                   string
	RemoteUrl             string
	Progress              string
	CompletionTokens      int
	TotalTokens           int
	UsageFacts            map[string]any
	PluginState           json.RawMessage
}
