package domain

import "github.com/QuantumNous/new-api/constant"

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type"`
	Key                string  `json:"key"`
	OpenAIOrganization *string `json:"openai_organization"`
	TestModel          *string `json:"test_model"`
	Status             int     `json:"status"`
	Name               string  `json:"name"`
	Weight             *uint   `json:"weight"`
	CreatedTime        int64   `json:"created_time"`
	TestTime           int64   `json:"test_time"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url"`
	Other              string  `json:"other"`
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time"`
	Models             string  `json:"models"`
	Group              string  `json:"group"`
	UsedQuota          int64   `json:"used_quota"`
	ModelMapping       *string `json:"model_mapping"`
	//MaxInputTokens     *int    `json:"max_input_tokens"`
	StatusCodeMapping *string `json:"status_code_mapping"`
	Priority          *int64  `json:"priority"`
	AutoBan           *int    `json:"auto_ban"`
	OtherInfo         string  `json:"other_info"`
	Tag               *string `json:"tag"`
	Setting           *string `json:"setting"` // extra channel settings
	ParamOverride     *string `json:"param_override"`
	HeaderOverride    *string `json:"header_override"`
	Remark            *string `json:"remark" validate:"max=255"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info"`

	OtherSettings string `json:"settings"` // other settings that are never searched (Azure version etc.), see dto.ChannelOtherSettings

	// cache info
	Keys []string `json:"-"`
}

type ChannelInfo struct {
	IsMultiKey             bool                  `json:"is_multi_key"`                        // whether the channel is in multi-key mode
	MultiKeySize           int                   `json:"multi_key_size"`                      // number of keys in multi-key mode
	MultiKeyStatusList     map[int]int           `json:"multi_key_status_list"`               // key status by key index
	MultiKeyDisabledReason map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key disable reason by key index
	MultiKeyDisabledTime   map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key disable time by key index
	MultiKeyPollingIndex   int                   `json:"multi_key_polling_index"`             // polling key index in multi-key mode
	MultiKeyMode           constant.MultiKeyMode `json:"multi_key_mode"`
}

func (channel *Channel) HideMultiKeyFailures() {
	if channel.ChannelInfo.IsMultiKey {
		channel.ChannelInfo.MultiKeyDisabledReason = nil
		channel.ChannelInfo.MultiKeyDisabledTime = nil
	}
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}
