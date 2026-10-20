package record

import "github.com/QuantumNous/new-api/internal/channels/domain"

func ToDomain(channel *Channel) *domain.Channel {
	if channel == nil {
		return nil
	}
	return &domain.Channel{Id: channel.Id, Type: channel.Type, Key: channel.Key, OpenAIOrganization: channel.OpenAIOrganization, TestModel: channel.TestModel, Status: channel.Status, Name: channel.Name, Weight: channel.Weight, CreatedTime: channel.CreatedTime, TestTime: channel.TestTime, ResponseTime: channel.ResponseTime, BaseURL: channel.BaseURL, Other: channel.Other, Balance: channel.Balance, BalanceUpdatedTime: channel.BalanceUpdatedTime, Models: channel.Models, Group: channel.Group, UsedQuota: channel.UsedQuota, ModelMapping: channel.ModelMapping, StatusCodeMapping: channel.StatusCodeMapping, Priority: channel.Priority, AutoBan: channel.AutoBan, OtherInfo: channel.OtherInfo, Tag: channel.Tag, Setting: channel.Setting, ParamOverride: channel.ParamOverride, HeaderOverride: channel.HeaderOverride, Remark: channel.Remark, ChannelInfo: domain.ChannelInfo{IsMultiKey: channel.ChannelInfo.IsMultiKey, MultiKeySize: channel.ChannelInfo.MultiKeySize, MultiKeyStatusList: channel.ChannelInfo.MultiKeyStatusList, MultiKeyDisabledReason: channel.ChannelInfo.MultiKeyDisabledReason, MultiKeyDisabledTime: channel.ChannelInfo.MultiKeyDisabledTime, MultiKeyPollingIndex: channel.ChannelInfo.MultiKeyPollingIndex, MultiKeyMode: channel.ChannelInfo.MultiKeyMode}, OtherSettings: channel.OtherSettings, Keys: channel.Keys}
}
func FromDomain(channel *domain.Channel) *Channel {
	if channel == nil {
		return nil
	}
	return &Channel{Id: channel.Id, Type: channel.Type, Key: channel.Key, OpenAIOrganization: channel.OpenAIOrganization, TestModel: channel.TestModel, Status: channel.Status, Name: channel.Name, Weight: channel.Weight, CreatedTime: channel.CreatedTime, TestTime: channel.TestTime, ResponseTime: channel.ResponseTime, BaseURL: channel.BaseURL, Other: channel.Other, Balance: channel.Balance, BalanceUpdatedTime: channel.BalanceUpdatedTime, Models: channel.Models, Group: channel.Group, UsedQuota: channel.UsedQuota, ModelMapping: channel.ModelMapping, StatusCodeMapping: channel.StatusCodeMapping, Priority: channel.Priority, AutoBan: channel.AutoBan, OtherInfo: channel.OtherInfo, Tag: channel.Tag, Setting: channel.Setting, ParamOverride: channel.ParamOverride, HeaderOverride: channel.HeaderOverride, Remark: channel.Remark, ChannelInfo: ChannelInfo{IsMultiKey: channel.ChannelInfo.IsMultiKey, MultiKeySize: channel.ChannelInfo.MultiKeySize, MultiKeyStatusList: channel.ChannelInfo.MultiKeyStatusList, MultiKeyDisabledReason: channel.ChannelInfo.MultiKeyDisabledReason, MultiKeyDisabledTime: channel.ChannelInfo.MultiKeyDisabledTime, MultiKeyPollingIndex: channel.ChannelInfo.MultiKeyPollingIndex, MultiKeyMode: channel.ChannelInfo.MultiKeyMode}, OtherSettings: channel.OtherSettings, Keys: channel.Keys}
}
