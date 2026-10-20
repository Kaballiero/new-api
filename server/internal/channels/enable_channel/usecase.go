package enable_channel

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Usecase struct {
	update func(int, string, int, string) bool
	notify func(string, string, string)
	log    func(string)
}

func NewUsecase(update func(int, string, int, string) bool, notify func(string, string, string), log func(string)) Usecase {
	return Usecase{update: update, notify: notify, log: log}
}
func (usecase Usecase) Execute(channelId int, usingKey string, channelName string) {
	success := usecase.update(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		usecase.notify(domain.ChannelStatusNotificationType(channelId, common.ChannelStatusEnabled), subject, content)
	}
}
