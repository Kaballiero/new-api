package preview_channel_models

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	domain.Configuration
	DecodeHeaderOverride(string) (map[string]any, error)
	Get(int, bool) (*domain.Channel, error)
	// EditAdvancedCustom reads the gateway settings of the channel and returns
	// the function that stores them back, with the advanced routes replaced by
	// the JSON in raw unless raw is nil.
	EditAdvancedCustom(channel *domain.Channel) func(raw *string) error
	// SetProxy stores proxy in the gateway settings of the channel.
	SetProxy(channel *domain.Channel, proxy string)
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
func (usecase Usecase) Execute(req FetchModelsRequest) (Output, error) {
	var channel *domain.Channel
	if req.Type == constant.ChannelTypeAdvancedCustom || req.ChannelID > 0 {
		var err error
		channel, err = usecase.Preview(req)
		if err != nil {
			return nil, domain.Reject(err)
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
		return nil, domain.ModelFetchFailed(err)
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
	saveAdvancedCustom := usecase.store.EditAdvancedCustom(channel)
	var rawConfig *string
	if req.AdvancedCustom != nil {
		trimmed := strings.TrimSpace(*req.AdvancedCustom)
		if trimmed == "" {
			return nil, fmt.Errorf("advanced_custom is required")
		}
		rawConfig = &trimmed
	} else if req.ChannelID <= 0 {
		return nil, fmt.Errorf("advanced_custom is required")
	}
	if err := saveAdvancedCustom(rawConfig); err != nil {
		return nil, err
	}
	if req.HeaderOverride != nil {
		rawHeaderOverride := strings.TrimSpace(*req.HeaderOverride)
		if rawHeaderOverride != "" {
			if _, err := usecase.store.DecodeHeaderOverride(rawHeaderOverride); err != nil {
				return nil, fmt.Errorf("header_override must be a JSON object: %w", err)
			}
		}
		channel.HeaderOverride = &rawHeaderOverride
	}
	if req.Proxy != nil {
		usecase.store.SetProxy(channel, strings.TrimSpace(*req.Proxy))
	}
	if err := domain.ValidateChannel(channel, false, usecase.store); err != nil {
		return nil, err
	}
	return channel, nil
}
