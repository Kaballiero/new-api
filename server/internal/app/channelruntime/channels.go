// Package channelruntime assembles channel status changes and root notifications.
package channelruntime

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	channelcore "github.com/QuantumNous/new-api/internal/app/channelcore"
	"github.com/QuantumNous/new-api/internal/app/notifications"
	"github.com/QuantumNous/new-api/internal/channels/disable_channel"
	"github.com/QuantumNous/new-api/internal/channels/enable_channel"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

var disable = sync.OnceValue(func() disable_channel.Usecase {
	return disable_channel.NewUsecase(channelcore.UpdateChannelStatus, rootChannelNotifier{}, common.SysLog, common.LocalLogPreview)
})
var enable = sync.OnceValue(func() enable_channel.Usecase {
	return enable_channel.NewUsecase(channelcore.UpdateChannelStatus, rootChannelNotifier{})
})

func Disable(input disable_channel.Input, reason string) { disable().Execute(input, reason) }
func Enable(id int, key, name string)                    { enable().Execute(id, key, name) }

// RootUserLanguage is the language of channel notifications and persisted
// channel texts produced without a request: the root user's language, or the
// default language when it is not set.
func RootUserLanguage() string {
	root := model.GetRootUser()
	if root == nil {
		return i18n.DefaultLang
	}
	return root.GetSetting().Language
}

// rootChannelNotifier sends channel status changes to the root user in the
// root user's language.
type rootChannelNotifier struct{}

func (rootChannelNotifier) ChannelDisabled(id int, name, reason string) {
	lang := RootUserLanguage()
	data := map[string]any{"Name": name, "ID": id, "Reason": reason}
	notifications.Root(channelStatusNotificationType(id, common.ChannelStatusAutoDisabled), i18n.Translate(lang, i18n.MsgChannelDisabledSubject, data), i18n.Translate(lang, i18n.MsgChannelDisabledContent, data))
}

func (rootChannelNotifier) ChannelEnabled(id int, name string) {
	notice := i18n.Translate(RootUserLanguage(), i18n.MsgChannelEnabledNotice, map[string]any{"Name": name, "ID": id})
	notifications.Root(channelStatusNotificationType(id, common.ChannelStatusEnabled), notice, notice)
}

func channelStatusNotificationType(id, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, id, status)
}
