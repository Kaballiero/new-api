package create_channel

import (
	"fmt"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"strings"
)

type Store interface {
	domain.Configuration
	OtherSettings(*domain.Channel) dto.ChannelOtherSettings
	Canonicalize(*domain.Channel) error
	InsertBatch([]domain.Channel) error
}
type Usecase struct {
	store         Store
	now           func() int64
	canBindPlugin func() bool
	audit         func(map[string]any)
}

func NewUsecase(store Store, now func() int64, canBindPlugin func() bool, audit func(map[string]any)) Usecase {
	return Usecase{store: store, now: now, canBindPlugin: canBindPlugin, audit: audit}
}
func (usecase Usecase) Execute(addChannelRequest AddChannelRequest) *domain.Failure {

	if addChannelRequest.Channel != nil && addChannelRequest.Channel.Type == constant.ChannelTypeTaskPlugin &&
		!usecase.canBindPlugin() {
		return &domain.Failure{Kind: "plugin_permission", Message: "task plugin channels require the task_plugin.bind permission"}
	}

	baseURLFromPluginDefault := addChannelRequest.Channel != nil &&
		addChannelRequest.Channel.Type == constant.ChannelTypeTaskPlugin &&
		(addChannelRequest.Channel.BaseURL == nil || strings.TrimSpace(*addChannelRequest.Channel.BaseURL) == "")
	// 使用统一的校验函数
	if err := domain.ValidateChannel(addChannelRequest.Channel, true, usecase.store); err != nil {
		return &domain.Failure{Kind: "channel_validation_failed", Message: err.Error()}
	}

	addChannelRequest.Channel.CreatedTime = usecase.now()
	keys := make([]string, 0)
	switch addChannelRequest.Mode {
	case "multi_to_single":
		addChannelRequest.Channel.ChannelInfo.IsMultiKey = true
		addChannelRequest.Channel.ChannelInfo.MultiKeyMode = addChannelRequest.MultiKeyMode
		if addChannelRequest.Channel.Type == constant.ChannelTypeVertexAi && usecase.store.OtherSettings(addChannelRequest.Channel).VertexKeyType != dto.VertexKeyTypeAPIKey {
			array, err := domain.VertexArrayKeys(addChannelRequest.Channel.Key)
			if err != nil {
				return &domain.Failure{Kind: "channel_validation_failed", Message: err.Error()}
			}
			addChannelRequest.Channel.ChannelInfo.MultiKeySize = len(array)
			addChannelRequest.Channel.Key = strings.Join(array, "\n")
		} else {
			cleanKeys := make([]string, 0)
			for _, key := range strings.Split(addChannelRequest.Channel.Key, "\n") {
				if key == "" {
					continue
				}
				key = strings.TrimSpace(key)
				cleanKeys = append(cleanKeys, key)
			}
			addChannelRequest.Channel.ChannelInfo.MultiKeySize = len(cleanKeys)
			addChannelRequest.Channel.Key = strings.Join(cleanKeys, "\n")
		}
		keys = []string{addChannelRequest.Channel.Key}
	case "batch":
		if addChannelRequest.Channel.Type == constant.ChannelTypeVertexAi && usecase.store.OtherSettings(addChannelRequest.Channel).VertexKeyType != dto.VertexKeyTypeAPIKey {
			// multi json
			var verr error
			keys, verr = domain.VertexArrayKeys(addChannelRequest.Channel.Key)
			if verr != nil {
				return &domain.Failure{Kind: "channel_validation_failed", Message: verr.Error()}
			}
		} else {
			keys = strings.Split(addChannelRequest.Channel.Key, "\n")
		}
	case "single":
		keys = []string{addChannelRequest.Channel.Key}
	default:
		return &domain.Failure{Kind: "mode_unsupported", Message: "不支持的添加模式"}
	}
	if err := usecase.store.Canonicalize(addChannelRequest.Channel); err != nil {
		return &domain.Failure{Kind: "channel_validation_failed", Message: err.Error()}
	}

	channels := make([]domain.Channel, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		localChannel := addChannelRequest.Channel
		localChannel.Key = key
		if addChannelRequest.BatchAddSetKeyPrefix2Name && len(keys) > 1 {
			keyPrefix := localChannel.Key
			if len(localChannel.Key) > 8 {
				keyPrefix = localChannel.Key[:8]
			}
			localChannel.Name = fmt.Sprintf("%s %s", localChannel.Name, keyPrefix)
		}
		channels = append(channels, *localChannel)
	}
	if err := usecase.store.InsertBatch(channels); err != nil {
		return &domain.Failure{Kind: "internal", Cause: err}
	}
	createAudit := map[string]any{
		"name":  addChannelRequest.Channel.Name,
		"type":  addChannelRequest.Channel.Type,
		"count": len(channels),
	}
	if baseURLFromPluginDefault {
		createAudit["base_url_source"] = "plugin_default"
	}
	usecase.audit(createAudit)
	return nil

}
