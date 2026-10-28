package domain

import (
	"fmt"

	"github.com/QuantumNous/new-api/constant"
)

func (channel *Channel) GetTag() string {
	if channel.Tag == nil {
		return ""
	}
	return *channel.Tag
}
func ApplyMultiKeyState(channel *Channel, keys []string, usingKey string, status int, reason string, now func() int64, otherInfo OtherInfoCodec, log func(string)) {
	if len(keys) == 0 {
		channel.Status = status
	} else {
		keyIndex := -1
		for i, key := range keys {
			if key == usingKey {
				keyIndex = i
				break
			}
		}
		if keyIndex < 0 {
			if usingKey != "" {
				log(fmt.Sprintf("failed to update multi-key status: channel_id=%d, using key not found", channel.Id))
				return
			}
			channel.Status = status
			info := otherInfo.ReadOtherInfo(channel)
			info["status_reason"] = reason
			info["status_time"] = now()
			otherInfo.WriteOtherInfo(channel, info)
			return
		}
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if status == constant.ChannelStatusEnabled {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		} else {
			channel.ChannelInfo.MultiKeyStatusList[keyIndex] = status
			if channel.ChannelInfo.MultiKeyDisabledReason == nil {
				channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
			}
			if channel.ChannelInfo.MultiKeyDisabledTime == nil {
				channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
			}
			channel.ChannelInfo.MultiKeyDisabledReason[keyIndex] = reason
			channel.ChannelInfo.MultiKeyDisabledTime[keyIndex] = now()
		}
		if !HasEnabledMultiKey(keys, channel.ChannelInfo.MultiKeyStatusList) {
			channel.Status = constant.ChannelStatusAutoDisabled
			info := otherInfo.ReadOtherInfo(channel)
			info["status_reason"] = "All keys are disabled"
			info["status_time"] = now()
			otherInfo.WriteOtherInfo(channel, info)
		} else if status == constant.ChannelStatusEnabled {
			channel.Status = constant.ChannelStatusEnabled
		}
	}
}
func HasEnabledMultiKey(keys []string, statusList map[int]int) bool {
	for i := range keys {
		if statusList == nil {
			return true
		}
		status, ok := statusList[i]
		if !ok || status == constant.ChannelStatusEnabled {
			return true
		}
	}
	return false
}
