package disable_channel

import (
	"fmt"
	"github.com/QuantumNous/new-api/constant"
)

// Input identifies the failed channel and the key the gateway used.
type Input struct {
	ChannelID   int
	ChannelName string
	UsingKey    string
	AutoBan     bool
}

// Notifier tells the administrators about a disabled channel.
type Notifier interface {
	ChannelDisabled(id int, name, reason string)
}

type Usecase struct {
	update   func(int, string, int, string) bool
	notifier Notifier
	log      func(string)
	preview  func(string) string
}

func NewUsecase(update func(int, string, int, string) bool, notifier Notifier, log func(string), preview func(string) string) Usecase {
	return Usecase{update: update, notifier: notifier, log: log, preview: preview}
}
func (usecase Usecase) Execute(input Input, reason string) {
	usecase.log(fmt.Sprintf("channel %q (#%d) failed and will be disabled, reason: %s", input.ChannelName, input.ChannelID, usecase.preview(reason)))
	if !input.AutoBan {
		usecase.log(fmt.Sprintf("channel %q (#%d) has automatic disabling turned off, skipping", input.ChannelName, input.ChannelID))
		return
	}
	success := usecase.update(input.ChannelID, input.UsingKey, constant.ChannelStatusAutoDisabled, reason)
	if success {
		usecase.notifier.ChannelDisabled(input.ChannelID, input.ChannelName, reason)
	}
}
