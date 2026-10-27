package controller

import (
	gatewayprobe "github.com/QuantumNous/new-api/internal/adapter/gateway/channels"
	"sync"

	"github.com/QuantumNous/new-api/internal/app"
	systemcore "github.com/QuantumNous/new-api/internal/app/system"
	"github.com/QuantumNous/new-api/internal/channels/detect_all_upstream_model_updates"
	"github.com/QuantumNous/new-api/internal/channels/list_channel_models"
	"github.com/QuantumNous/new-api/internal/channels/test_channels"
	"github.com/QuantumNous/new-api/internal/system/enqueue_system_task"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/modelcatalog"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

var channelsHTTP = sync.OnceValue(func() app.ChannelsHTTP { return app.NewChannelsHTTP(channelEffects()) })

func channelEffects() app.ChannelEffects {
	return app.ChannelEffects{ModelUpdateThrottle: shouldSendUpstreamModelUpdateNotification, EnqueueModelUpdate: func() (detect_all_upstream_model_updates.Task, bool, error) {
		queued, err := systemcore.Default().Enqueue.Execute(enqueue_system_task.Input{Type: model.SystemTaskTypeModelUpdate, Payload: map[string]bool{"manual": true}})
		task, created := queued.Task, queued.Created
		if task == nil {
			return detect_all_upstream_model_updates.Task{}, created, err
		}
		return detect_all_upstream_model_updates.Task{ID: task.TaskID, Status: string(task.Status), Type: task.Type}, created, err
	}, FetchModels: func(channel *model.Channel) ([]string, error) { return fetchChannelUpstreamModelIDsFn(channel) }, RefreshRuntime: refreshChannelRuntimeCache, AutomaticStream: gatewayprobe.AutomaticStream, EnqueueTest: func() (test_channels.Task, bool, error) {
		queued, err := systemcore.Default().Enqueue.Execute(enqueue_system_task.Input{Type: model.SystemTaskTypeChannelTest, Payload: map[string]any{"mode": operation_setting.ChannelTestModeScheduledAll, "notify": true}})
		task, created := queued.Task, queued.Created
		if task == nil {
			return test_channels.Task{}, created, err
		}
		return test_channels.Task{ID: task.TaskID, Status: string(task.Status), Type: task.Type}, created, err
	}, ProbeUser: gatewayprobe.ResolveProbeUserID, Probe: gatewayprobe.NewEngine,
		Audit: func(c *gin.Context, action string, params map[string]any) { recordManageAudit(c, action, params) }, ReadAudit: markAuditLogged, SecurityError: writeSecurityOperationError, Refresh: refreshChannelPricingCache,
		ChannelListModels: func() list_channel_models.Output {
			openAIModels := modelcatalog.Models()
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
