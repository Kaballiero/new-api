package transport

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"
	"github.com/QuantumNous/new-api/internal/app/channelruntime"
	"github.com/QuantumNous/new-api/internal/channels/disable_channel"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	usagegateway "github.com/QuantumNous/new-api/relay/usage"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

func ProcessChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError, relayInfo *relaycommon.RelayInfo) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if channeladapter.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			channelruntime.Disable(disable_channel.Input{ChannelID: channelError.ChannelId, ChannelName: channelError.ChannelName, UsingKey: channelError.UsingKey, AutoBan: channelError.AutoBan}, err.ErrorWithStatusCode())
		})
	}

	usagegateway.RecordUpstreamFailure(c, channelError.ChannelId, err, relayInfo)

}
