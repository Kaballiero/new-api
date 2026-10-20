package service

import (
	"github.com/QuantumNous/new-api/internal/channels/disable_channel"
	"github.com/QuantumNous/new-api/internal/channels/enable_channel"

	"github.com/QuantumNous/new-api/internal/channels/domain"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func formatNotifyType(id, status int) string { return domain.ChannelStatusNotificationType(id, status) }

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	disable_channel.NewUsecase(model.UpdateChannelStatus, NotifyRootUser, common.SysLog).Execute(channelError, reason)
}

func EnableChannel(id int, key, name string) {
	enable_channel.NewUsecase(model.UpdateChannelStatus, NotifyRootUser, common.SysLog).Execute(id, key, name)
}

func channelFailurePolicy() domain.FailurePolicy {
	return domain.FailurePolicy{DisableEnabled: func() bool { return common.AutomaticDisableChannelEnabled }, EnableEnabled: func() bool { return common.AutomaticEnableChannelEnabled }, DisableStatus: operation_setting.ShouldDisableByStatusCode, Keywords: func() []string { return operation_setting.AutomaticDisableKeywords }, Search: AcSearch}
}
func ShouldDisableChannel(err *types.NewAPIError) bool {
	return channelFailurePolicy().ShouldDisable(err)
}
func ShouldEnableChannel(err *types.NewAPIError, status int) bool {
	return channelFailurePolicy().ShouldEnable(err, status)
}
