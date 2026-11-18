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

// localErrorCodes name the failures new-api itself decides for the user or the
// request: a request it cannot read, accept or convert, quota the user or the
// API key lacks, and billing data it cannot read or write. The channel has no
// part in them.
var localErrorCodes = map[types.ErrorCode]struct{}{
	types.ErrorCodeInvalidRequest:             {},
	types.ErrorCodeReadRequestBodyFailed:      {},
	types.ErrorCodeConvertRequestFailed:       {},
	types.ErrorCodeInsufficientUserQuota:      {},
	types.ErrorCodePreConsumeTokenQuotaFailed: {},
	types.ErrorCodeQueryDataError:             {},
	types.ErrorCodeUpdateDataError:            {},
}

// isLocalError reports a failure new-api decided itself, which never counts
// against a channel. An upstream error that carries the same code is not local.
func isLocalError(err *types.NewAPIError) bool {
	if err.GetErrorType() != types.ErrorTypeNewAPIError {
		return false
	}
	_, local := localErrorCodes[err.GetErrorCode()]
	return local
}

func ProcessChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError, relayInfo *relaycommon.RelayInfo) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if channeladapter.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			channelruntime.Disable(disable_channel.Input{ChannelID: channelError.ChannelId, ChannelName: channelError.ChannelName, UsingKey: channelError.UsingKey, AutoBan: channelError.AutoBan}, err.ErrorWithStatusCode())
		})
	}

	// The refund of an unknown OpenRouter outcome records this error itself.
	if !relaycommon.IsOpenRouterUnknownOutcomeRefund(relayInfo) {
		usagegateway.RecordUpstreamFailure(c, channelError.ChannelId, err, relayInfo)
	}

}
