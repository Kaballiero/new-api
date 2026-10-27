package channels

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	gatewaytransport "github.com/QuantumNous/new-api/gateway/transport"
	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func NewEngine(resolve func(*channeldomain.Channel) *model.Channel) channeldomain.Probe {
	return channeladapter.NewProbeEngine(func(ctx context.Context, channel *channeldomain.Channel, userID int, modelName, endpoint string, stream bool) channeldomain.ProbeOutcome {
		record := resolve(channel)
		result := testChannel(ctx, record, userID, modelName, endpoint, stream)
		key := ""
		if result.context != nil {
			key = common.GetContextKeyString(result.context, constant.ContextKeyChannelKey)
		}
		return channeladapter.NewProbeOutcome(result.localErr, result.newAPIError, key, func(apiErr *types.NewAPIError) {
			gatewaytransport.ProcessChannelError(result.context, *types.NewChannelError(record.Id, record.Type, record.Name, record.ChannelInfo.IsMultiKey, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), record.GetAutoBan()), apiErr, nil)
		})
	})
}
