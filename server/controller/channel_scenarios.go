package controller

import (
	"context"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/internal/channels/detect_all_upstream_model_updates"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/list_channel_models"
	"github.com/QuantumNous/new-api/internal/channels/test_channels"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

var channelsHTTP = sync.OnceValue(func() app.ChannelsHTTP { return app.NewChannelsHTTP(channelEffects()) })

func channelEffects() app.ChannelEffects {
	return app.ChannelEffects{ModelUpdateThrottle: shouldSendUpstreamModelUpdateNotification, EnqueueModelUpdate: func() (detect_all_upstream_model_updates.Task, bool, error) {
		task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeModelUpdate, modelUpdateTaskPayload{Manual: true})
		if task == nil {
			return detect_all_upstream_model_updates.Task{}, created, err
		}
		return detect_all_upstream_model_updates.Task{ID: task.TaskID, Status: string(task.Status), Type: task.Type}, created, err
	}, FetchModels: func(channel *model.Channel) ([]string, error) { return fetchChannelUpstreamModelIDsFn(channel) }, RefreshRuntime: refreshChannelRuntimeCache, AutomaticStream: shouldUseStreamForAutomaticChannelTest, EnqueueTest: func() (test_channels.Task, bool, error) {
		task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeChannelTest, channelTestTaskPayload{Mode: operation_setting.ChannelTestModeScheduledAll, Notify: true})
		if task == nil {
			return test_channels.Task{}, created, err
		}
		return test_channels.Task{ID: task.TaskID, Status: string(task.Status), Type: task.Type}, created, err
	}, ProbeUser: resolveChannelTestUserID, Probe: func(resolve func(*channeldomain.Channel) *model.Channel) channeldomain.Probe {
		return channeladapter.NewProbeEngine(func(ctx context.Context, channel *channeldomain.Channel, userID int, modelName, endpoint string, stream bool) channeldomain.ProbeOutcome {
			record := resolve(channel)
			result := testChannel(ctx, record, userID, modelName, endpoint, stream)
			key := ""
			if result.context != nil {
				key = common.GetContextKeyString(result.context, constant.ContextKeyChannelKey)
			}
			return channeladapter.NewProbeOutcome(result.localErr, result.newAPIError, key, func(apiErr *types.NewAPIError) {
				processChannelError(result.context, *types.NewChannelError(record.Id, record.Type, record.Name, record.ChannelInfo.IsMultiKey, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), record.GetAutoBan()), apiErr, nil)
			})
		})
	},
		Audit: func(c *gin.Context, action string, params map[string]any) { recordManageAudit(c, action, params) }, ReadAudit: markAuditLogged, SecurityError: writeSecurityOperationError, Refresh: refreshChannelPricingCache,
		ChannelListModels: func() list_channel_models.Output {
			if openAIModels == nil {
				return nil
			}
			models := make(list_channel_models.Output, len(openAIModels))
			for i, item := range openAIModels {
				models[i] = list_channel_models.Model{Id: item.Id, Object: item.Object, Created: item.Created, OwnedBy: item.OwnedBy}
				if item.SupportedEndpointTypes != nil {
					models[i].SupportedEndpointTypes = make([]string, len(item.SupportedEndpointTypes))
					for j, endpoint := range item.SupportedEndpointTypes {
						models[i].SupportedEndpointTypes[j] = string(endpoint)
					}
				}
			}
			return models
		},
	}
}
