package domain

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

// Configuration reads the gateway-owned parts of a channel configuration.
type Configuration interface {
	DecodeVertexRegions(string) (map[string]any, error)
	DecodeCodexFields(string) (map[string]any, error)
	// ValidateSettings checks the gateway settings stored in the channel.
	ValidateSettings(*Channel) error
	// TaskPluginKey returns the task plugin bound by the channel settings.
	TaskPluginKey(*Channel) string
	// PluginBaseURL returns the default base URL of a registered task plugin.
	PluginBaseURL(string) (string, bool)
}

func (channel *Channel) BaseURLValue() string {
	if channel.BaseURL == nil {
		return ""
	}
	url := *channel.BaseURL
	if url == "" {
		url = constant.GetChannelBaseURL(channel.Type)
	}
	return url
}
func (channel *Channel) GetModels() []string {
	if channel.Models == "" {
		return []string{}
	}
	return strings.Split(strings.Trim(channel.Models, ","), ",")
}
func ValidateChannel(channel *Channel, isAdd bool, config Configuration) error {
	if channel == nil {
		return ErrChannelRequired
	}

	if err := config.ValidateSettings(channel); err != nil {
		return &DetailError{Err: ErrChannelSettingInvalid, Detail: err.Error()}
	}
	if channel.Type == constant.ChannelTypeTaskPlugin {
		pluginKey := strings.TrimSpace(config.TaskPluginKey(channel))
		if pluginKey == "" {
			return ErrTaskPluginKeyRequired
		}
		if len(pluginKey) > 30 {
			return ErrTaskPluginKeyTooLong
		}
		plugin, ok := config.PluginBaseURL(pluginKey)
		if !ok {
			return fmt.Errorf("task plugin %q is not registered", pluginKey)
		}
		if channel.BaseURL == nil || strings.TrimSpace(*channel.BaseURL) == "" {
			if plugin == "" {
				return ErrTaskPluginBaseURLRequired
			}
			defaultBaseURL := plugin
			channel.BaseURL = &defaultBaseURL
		}
	}

	if channel.Type == constant.ChannelTypeNewAPI && strings.TrimSpace(channel.BaseURLValue()) == "" {
		return ErrNewAPIBaseURLRequired
	}

	// A new channel needs a key and model names that fit the column.
	if isAdd {
		if channel.Key == "" {
			return ErrChannelRequired
		}

		for _, m := range channel.GetModels() {
			if len(m) > 255 {
				return &DetailError{Err: ErrModelNameTooLong, Detail: m}
			}
		}
	}

	// Vertex AI needs a JSON map of deployment regions with a default entry.
	if channel.Type == constant.ChannelTypeVertexAi {
		if channel.Other == "" {
			return ErrVertexRegionRequired
		}

		regionMap, err := config.DecodeVertexRegions(channel.Other)
		if err != nil {
			return ErrVertexRegionInvalid
		}

		if regionMap["default"] == nil {
			return ErrVertexRegionDefaultMissing
		}
	}

	// Codex OAuth key validation (optional, only when JSON object is provided)
	if channel.Type == constant.ChannelTypeCodex {
		trimmedKey := strings.TrimSpace(channel.Key)
		if isAdd || trimmedKey != "" {
			if !strings.HasPrefix(trimmedKey, "{") {
				return ErrCodexKeyInvalid
			}
			keyMap, err := config.DecodeCodexFields(trimmedKey)
			if err != nil {
				return ErrCodexKeyInvalid
			}
			if v, ok := keyMap["access_token"]; !ok || v == nil || strings.TrimSpace(fmt.Sprintf("%v", v)) == "" {
				return ErrCodexKeyAccessTokenRequired
			}
			if v, ok := keyMap["account_id"]; !ok || v == nil || strings.TrimSpace(fmt.Sprintf("%v", v)) == "" {
				return ErrCodexKeyAccountIDRequired
			}
		}
	}

	return nil
}
