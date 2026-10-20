package preview_channel_models

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"strings"
)

type Store interface {
	domain.Configuration
	Get(int, bool) (*domain.Channel, error)
	OtherSettings(*domain.Channel) dto.ChannelOtherSettings
	SetOtherSettings(*domain.Channel, dto.ChannelOtherSettings)
	SetSettings(*domain.Channel, dto.ChannelSettings)
}
type Discovery interface {
	Fetch(*domain.Channel) ([]string, error)
}
type Usecase struct {
	store     Store
	discovery Discovery
}

func NewUsecase(store Store, discovery Discovery) Usecase {
	return Usecase{store: store, discovery: discovery}
}
func (usecase Usecase) Execute(req FetchModelsRequest) (Output, *domain.Failure) {
	var channel *domain.Channel
	if req.Type == constant.ChannelTypeAdvancedCustom || req.ChannelID > 0 {
		var err error
		channel, err = usecase.Preview(req)
		if err != nil {
			return nil, &domain.Failure{Kind: "invalid", Message: err.Error()}
		}
	} else {
		baseURL := ""
		if req.BaseURL != nil {
			baseURL = strings.TrimSpace(*req.BaseURL)
		}
		if baseURL == "" {
			baseURL = constant.GetChannelBaseURL(req.Type)
		}

		key := strings.TrimSpace(req.Key)
		if req.Type != constant.ChannelTypeCodex {
			key = strings.Split(key, "\n")[0]
		}
		channel = &domain.Channel{
			Type:    req.Type,
			Key:     key,
			BaseURL: &baseURL,
		}
	}

	models, err := usecase.discovery.Fetch(channel)
	if err != nil {
		return nil, &domain.Failure{Kind: "fetch", Message: fmt.Sprintf("获取模型列表失败: %s", err.Error())}
	}
	return models, nil
}
func (usecase Usecase) Preview(req FetchModelsRequest) (*domain.Channel, error) {
	var channel *domain.Channel
	if req.ChannelID > 0 {
		savedChannel, err := usecase.store.Get(req.ChannelID, true)
		if err != nil {
			return nil, err
		}
		if savedChannel.Type != constant.ChannelTypeAdvancedCustom {
			return nil, fmt.Errorf("channel %d is not an advanced custom channel", req.ChannelID)
		}
		channel = savedChannel
	} else {
		key := strings.TrimSpace(req.Key)
		if key != "" {
			key = strings.Split(key, "\n")[0]
		}
		channel = &domain.Channel{
			Type: req.Type,
			Key:  key,
		}
	}

	if channel.Type != constant.ChannelTypeAdvancedCustom {
		return nil, fmt.Errorf("channel type must be advanced custom")
	}
	if req.BaseURL != nil {
		baseURL := strings.TrimSpace(*req.BaseURL)
		channel.BaseURL = &baseURL
	}

	settings := usecase.store.OtherSettings(channel)
	if req.AdvancedCustom != nil {
		rawConfig := strings.TrimSpace(*req.AdvancedCustom)
		if rawConfig == "" {
			return nil, fmt.Errorf("advanced_custom is required")
		}
		var config dto.AdvancedCustomConfig
		if err := common.UnmarshalJsonStr(rawConfig, &config); err != nil {
			return nil, err
		}
		settings.AdvancedCustom = &config
	} else if req.ChannelID <= 0 {
		return nil, fmt.Errorf("advanced_custom is required")
	}
	usecase.store.SetOtherSettings(channel, settings)

	if req.HeaderOverride != nil {
		rawHeaderOverride := strings.TrimSpace(*req.HeaderOverride)
		if rawHeaderOverride != "" {
			var headerOverride map[string]any
			if err := common.UnmarshalJsonStr(rawHeaderOverride, &headerOverride); err != nil {
				return nil, fmt.Errorf("header_override must be a JSON object: %w", err)
			}
		}
		channel.HeaderOverride = &rawHeaderOverride
	}
	if req.Proxy != nil {
		channelSettings := usecase.store.Settings(channel)
		channelSettings.Proxy = strings.TrimSpace(*req.Proxy)
		usecase.store.SetSettings(channel, channelSettings)
	}

	if err := domain.ValidateChannel(channel, false, usecase.store); err != nil {
		return nil, err
	}
	return channel, nil
}
