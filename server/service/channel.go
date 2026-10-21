package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/channels/disable_channel"
	"github.com/QuantumNous/new-api/internal/channels/enable_channel"

	"github.com/QuantumNous/new-api/internal/channels/domain"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	input := disable_channel.Input{ChannelID: channelError.ChannelId, ChannelName: channelError.ChannelName, UsingKey: channelError.UsingKey, AutoBan: channelError.AutoBan}
	disable_channel.NewUsecase(model.UpdateChannelStatus, rootChannelNotifier{}, common.SysLog).Execute(input, reason)
}

func EnableChannel(id int, key, name string) {
	enable_channel.NewUsecase(model.UpdateChannelStatus, rootChannelNotifier{}).Execute(id, key, name)
}

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
	NotifyRootUser(channelStatusNotificationType(id, common.ChannelStatusAutoDisabled), i18n.Translate(lang, i18n.MsgChannelDisabledSubject, data), i18n.Translate(lang, i18n.MsgChannelDisabledContent, data))
}

func (rootChannelNotifier) ChannelEnabled(id int, name string) {
	notice := i18n.Translate(RootUserLanguage(), i18n.MsgChannelEnabledNotice, map[string]any{"Name": name, "ID": id})
	NotifyRootUser(channelStatusNotificationType(id, common.ChannelStatusEnabled), notice, notice)
}

func channelStatusNotificationType(id, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, id, status)
}

func channelFailurePolicy() domain.FailurePolicy {
	return domain.FailurePolicy{DisableEnabled: func() bool { return common.AutomaticDisableChannelEnabled }, EnableEnabled: func() bool { return common.AutomaticEnableChannelEnabled }, DisableStatus: operation_setting.ShouldDisableByStatusCode, Keywords: func() []string { return operation_setting.AutomaticDisableKeywords }, Search: AcSearch}
}

// channelFailure describes a gateway error for the channel failure policy.
func channelFailure(err *types.NewAPIError) *domain.ChannelFailure {
	if err == nil {
		return nil
	}
	return &domain.ChannelFailure{Message: err.Error(), Status: err.StatusCode, ChannelError: types.IsChannelError(err), SkipRetry: types.IsSkipRetryError(err)}
}

func ShouldDisableChannel(err *types.NewAPIError) bool {
	return channelFailurePolicy().ShouldDisable(channelFailure(err))
}
func ShouldEnableChannel(err *types.NewAPIError, status int) bool {
	return channelFailurePolicy().ShouldEnable(err != nil, status)
}
