package domain

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"strings"
)

type ModelFetchFingerprint struct {
	Type               int
	Key                string
	EffectiveBaseURL   string
	Proxy              string
	HeaderOverride     string
	AdvancedCustom     *dto.AdvancedCustomConfig
	TaskPluginKey      string
	IsMultiKey         bool
	MultiKeySize       int
	MultiKeyStatusList map[int]int
	DisabledReason     map[int]string
	DisabledTime       map[int]int64
	MultiKeyMode       constant.MultiKeyMode
}

func BuildModelFetchFingerprint(channel *Channel) (ModelFetchFingerprint, error) {
	if channel == nil {
		return ModelFetchFingerprint{}, fmt.Errorf("channel is nil")
	}

	setting := dto.ChannelSettings{}
	if channel.Setting != nil && strings.TrimSpace(*channel.Setting) != "" {
		if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
			return ModelFetchFingerprint{}, fmt.Errorf("invalid channel setting: %w", err)
		}
	}

	effectiveBaseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.BaseURLValue() != "" {
		effectiveBaseURL = channel.BaseURLValue()
	}
	headerOverride := ""
	if channel.HeaderOverride != nil {
		headerOverride = *channel.HeaderOverride
	}

	statusList := make(map[int]int, len(channel.ChannelInfo.MultiKeyStatusList))
	for index, status := range channel.ChannelInfo.MultiKeyStatusList {
		statusList[index] = status
	}
	disabledReason := make(map[int]string, len(channel.ChannelInfo.MultiKeyDisabledReason))
	for index, reason := range channel.ChannelInfo.MultiKeyDisabledReason {
		disabledReason[index] = reason
	}
	disabledTime := make(map[int]int64, len(channel.ChannelInfo.MultiKeyDisabledTime))
	for index, disabledAt := range channel.ChannelInfo.MultiKeyDisabledTime {
		disabledTime[index] = disabledAt
	}

	return ModelFetchFingerprint{
		Type:               channel.Type,
		Key:                channel.Key,
		EffectiveBaseURL:   effectiveBaseURL,
		Proxy:              setting.Proxy,
		HeaderOverride:     headerOverride,
		AdvancedCustom:     nil,
		TaskPluginKey:      setting.TaskPluginKey,
		IsMultiKey:         channel.ChannelInfo.IsMultiKey,
		MultiKeySize:       channel.ChannelInfo.MultiKeySize,
		MultiKeyStatusList: statusList,
		DisabledReason:     disabledReason,
		DisabledTime:       disabledTime,
		MultiKeyMode:       channel.ChannelInfo.MultiKeyMode,
	}, nil
}
