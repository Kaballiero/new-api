package enable_channel

import "github.com/QuantumNous/new-api/constant"

// Notifier tells the administrators about a re-enabled channel.
type Notifier interface {
	ChannelEnabled(id int, name string)
}

type Usecase struct {
	update   func(int, string, int, string) bool
	notifier Notifier
}

func NewUsecase(update func(int, string, int, string) bool, notifier Notifier) Usecase {
	return Usecase{update: update, notifier: notifier}
}
func (usecase Usecase) Execute(channelId int, usingKey string, channelName string) {
	success := usecase.update(channelId, usingKey, constant.ChannelStatusEnabled, "")
	if success {
		usecase.notifier.ChannelEnabled(channelId, channelName)
	}
}
