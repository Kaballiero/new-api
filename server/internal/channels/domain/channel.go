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
	Setting           *string `json:"setting"` // 渠道额外设置
	ParamOverride     *string `json:"param_override"`
	HeaderOverride    *string `json:"header_override"`
	Remark            *string `json:"remark" validate:"max=255"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info"`

	OtherSettings string `json:"settings"` // 其他设置，存储azure版本等不需要检索的信息，详见dto.ChannelOtherSettings

	// cache info
	Keys []string `json:"-"`
}

type ChannelInfo struct {
	IsMultiKey             bool                  `json:"is_multi_key"`                        // 是否多Key模式
	MultiKeySize           int                   `json:"multi_key_size"`                      // 多Key模式下的Key数量
	MultiKeyStatusList     map[int]int           `json:"multi_key_status_list"`               // key状态列表，key index -> status
	MultiKeyDisabledReason map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key禁用原因列表，key index -> reason
	MultiKeyDisabledTime   map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key禁用时间列表，key index -> time
	MultiKeyPollingIndex   int                   `json:"multi_key_polling_index"`             // 多Key模式下轮询的key索引
	MultiKeyMode           constant.MultiKeyMode `json:"multi_key_mode"`
}

func (channel *Channel) HideMultiKeyFailures() {
	if channel.ChannelInfo.IsMultiKey {
		channel.ChannelInfo.MultiKeyDisabledReason = nil
		channel.ChannelInfo.MultiKeyDisabledTime = nil
	}
}

type Failure struct {
	Kind    string
	Cause   error
	Message string
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}
