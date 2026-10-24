package taskplugins

import (
	"fmt"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/model"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/taskregistry"
)

// Artifacts maps the task owner's snapshots to existing plugin hooks.
type Artifacts struct{}

func (Artifacts) ListArtifacts(task *domain.Task) ([]domain.Artifact, error) {
	adaptor := taskregistry.GetTaskAdaptor(constant.TaskPlatform(task.Platform))
	if adaptor == nil {
		return nil, domain.ErrArtifactPluginUnavailable
	}
	provider, ok := adaptor.(relaychannel.TaskArtifactProvider)
	if !ok {
		return []domain.Artifact{}, nil
	}
	artifacts, err := provider.ListArtifacts(record.FromDomain(task))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrArtifactPlugin, err)
	}
	result := make([]domain.Artifact, len(artifacts))
	for i, artifact := range artifacts {
		result[i] = domain.Artifact{Key: artifact.Key, Type: artifact.Type, MimeType: artifact.MimeType}
	}
	return result, nil
}
func (Artifacts) BuildContentRequest(task *domain.Task, key string, client domain.MediaClientRequest) (*domain.MediaRequest, bool, error) {
	if task == nil || !domain.HasPluginExecution(task) {
		return nil, false, domain.ErrArtifactPluginUnavailable
	}
	channelModel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		return nil, false, fmt.Errorf("%w: channel unavailable", domain.ErrArtifactPluginUnavailable)
	}
	adaptor := taskregistry.GetTaskAdaptor(constant.TaskPlatform(task.Platform))
	if adaptor == nil {
		return nil, false, domain.ErrArtifactPluginUnavailable
	}
	pluginKey := task.PrivateData.Key
	if pluginKey == "" {
		pluginKey = channelModel.Key
	}
	baseURL := channelModel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.GetChannelBaseURL(channelModel.Type)
	}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelModel.Type, ChannelBaseUrl: baseURL, ApiKey: pluginKey, ChannelSetting: channelModel.GetSetting()}})
	provider, ok := adaptor.(relaychannel.TaskContentRequestProvider)
	if !ok {
		return nil, false, nil
	}
	descriptor, err := provider.BuildContentRequest(record.FromDomain(task), key, relaychannel.TaskArtifactClientRequest{
		Method:  client.Method,
		Headers: client.Headers,
	})
	if err != nil {
		return nil, true, domain.ErrArtifactContent
	}
	if descriptor == nil {
		return nil, true, nil
	}
	return &domain.MediaRequest{
		URL:            descriptor.URL,
		Method:         descriptor.Method,
		Headers:        descriptor.Headers,
		Body:           descriptor.Body,
		Credentialless: descriptor.Credentialless,
	}, true, nil
}
