package channelcore

import (
	"github.com/QuantumNous/new-api/common"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/channels/change_channel_state"
)

func UpdateChannelStatus(id int, key string, status int, reason string) bool {
	return change_channel_state.NewUsecase(
		channelstore.NewChannelPersistence(),
		func() bool { return common.MemoryCacheEnabled },
		common.GetTimestamp,
		common.SysLog,
	).Execute(
		id,
		key,
		status,
		reason,
	)
}

type StateWriter struct{}

func (StateWriter) ChangeStatus(id, status int, reason string) bool {
	return UpdateChannelStatus(id, "", status, reason)
}
