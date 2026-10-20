package disable_channel

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type Usecase struct {
	update func(int, string, int, string) bool
	notify func(string, string, string)
	log    func(string)
}

func NewUsecase(update func(int, string, int, string) bool, notify func(string, string, string), log func(string)) Usecase {
	return Usecase{update: update, notify: notify, log: log}
}
func (usecase Usecase) Execute(channelError types.ChannelError, reason string) {
	usecase.log(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		usecase.log(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := usecase.update(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelError.ChannelName, channelError.ChannelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason)
		usecase.notify(domain.ChannelStatusNotificationType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
	}
}
