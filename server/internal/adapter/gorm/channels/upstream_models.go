package channels

import (
	"fmt"
	"maps"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"gorm.io/gorm"
)

var channelUpstreamModelUpdateSelectFields = []string{
	"id",
	"name",
	"type",
	"key",
	"status",
	"base_url",
	"models",
	"model_mapping",
	"settings",
	"setting",
	"other",
	"group",
	"priority",
	"weight",
	"tag",
	"channel_info",
	"header_override",
}

// ModelUpdateState reads the upstream model update state from the channel
// settings without repairing invalid settings.
func (Store) ModelUpdateState(channel *domain.Channel) (domain.ModelUpdateState, error) {
	settings, err := parseOtherSettings(channel)
	if err != nil {
		return domain.ModelUpdateState{}, err
	}
	return domain.ModelUpdateState{
		CheckEnabled:       settings.UpstreamModelUpdateCheckEnabled,
		AutoSyncEnabled:    settings.UpstreamModelUpdateAutoSyncEnabled,
		LastCheckTime:      settings.UpstreamModelUpdateLastCheckTime,
		LastDetectedModels: settings.UpstreamModelUpdateLastDetectedModels,
		LastRemovedModels:  settings.UpstreamModelUpdateLastRemovedModels,
		IgnoredModels:      settings.UpstreamModelUpdateIgnoredModels,
	}, nil
}

func parseOtherSettings(channel *domain.Channel) (dto.ChannelOtherSettings, error) {
	settings := dto.ChannelOtherSettings{}
	if channel == nil {
		return settings, fmt.Errorf("channel is nil")
	}
	if channel.OtherSettings == "" {
		return settings, nil
	}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
		return dto.ChannelOtherSettings{}, fmt.Errorf("invalid channel other settings: %w", err)
	}
	return settings, nil
}

// FetchFingerprint returns the configuration that decides where and how the
// upstream models of the channel are fetched.
func (store Store) FetchFingerprint(channel *domain.Channel) (domain.ModelFetchFingerprint, error) {
	if channel == nil {
		return domain.ModelFetchFingerprint{}, fmt.Errorf("channel is nil")
	}

	setting := dto.ChannelSettings{}
	if channel.Setting != nil && strings.TrimSpace(*channel.Setting) != "" {
		if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
			return domain.ModelFetchFingerprint{}, fmt.Errorf("invalid channel setting: %w", err)
		}
	}

	effectiveBaseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.BaseURLValue() != "" {
		effectiveBaseURL = channel.BaseURLValue()
	}
	headerOverride := ""
	if channel.HeaderOverride != nil {
		headerOverride = *channel.HeaderOverride
	}

	// The maps are copied into non-nil maps so that a missing and an empty
	// status compare equal.
	statusList := make(map[int]int, len(channel.ChannelInfo.MultiKeyStatusList))
	maps.Copy(statusList, channel.ChannelInfo.MultiKeyStatusList)
	disabledReason := make(map[int]string, len(channel.ChannelInfo.MultiKeyDisabledReason))
	maps.Copy(disabledReason, channel.ChannelInfo.MultiKeyDisabledReason)
	disabledTime := make(map[int]int64, len(channel.ChannelInfo.MultiKeyDisabledTime))
	maps.Copy(disabledTime, channel.ChannelInfo.MultiKeyDisabledTime)

	fingerprint := domain.ModelFetchFingerprint{
		Type:               channel.Type,
		Key:                channel.Key,
		EffectiveBaseURL:   effectiveBaseURL,
		Proxy:              setting.Proxy,
		HeaderOverride:     headerOverride,
		TaskPluginKey:      setting.TaskPluginKey,
		IsMultiKey:         channel.ChannelInfo.IsMultiKey,
		MultiKeySize:       channel.ChannelInfo.MultiKeySize,
		MultiKeyStatusList: statusList,
		DisabledReason:     disabledReason,
		DisabledTime:       disabledTime,
		MultiKeyMode:       channel.ChannelInfo.MultiKeyMode,
	}
	record := store.MutableRecord(channel)
	fingerprint.AdvancedCustom = record.GetOtherSettings().AdvancedCustom
	*channel = *ToDomain(record)
	return fingerprint, nil
}

// WithLockedModelState applies the upstream model update state over the
// other settings of the locked channel.
func (store Store) WithLockedModelState(id int, apply func(*domain.Channel, func(domain.ModelUpdateState, bool) error) error) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := model.GetChannelByIdForUpdate(tx, id)
		if err != nil {
			return err
		}
		channel := store.track(locked)
		return apply(channel, func(state domain.ModelUpdateState, changed bool) error {
			settings, err := parseOtherSettings(channel)
			if err != nil {
				return err
			}
			settings.UpstreamModelUpdateCheckEnabled = state.CheckEnabled
			settings.UpstreamModelUpdateAutoSyncEnabled = state.AutoSyncEnabled
			settings.UpstreamModelUpdateLastCheckTime = state.LastCheckTime
			settings.UpstreamModelUpdateLastDetectedModels = state.LastDetectedModels
			settings.UpstreamModelUpdateLastRemovedModels = state.LastRemovedModels
			settings.UpstreamModelUpdateIgnoredModels = state.IgnoredModels
			record := store.MutableRecord(channel)
			record.SetOtherSettings(settings)
			*channel = *ToDomain(record)
			updates := map[string]any{"settings": record.OtherSettings}
			if changed {
				updates["models"] = record.Models
				updates["model_mapping"] = record.ModelMapping
			}
			if err := tx.Model(&model.Channel{}).Where("id = ?", record.Id).Updates(updates).Error; err != nil {
				return err
			}
			if changed {
				return record.UpdateAbilities(tx)
			}
			return nil
		})
	})
}
func (store Store) EnabledAfter(lastID, batchSize int) ([]*domain.Channel, error) {
	var records []*model.Channel
	query := model.DB.Select(channelUpstreamModelUpdateSelectFields).Where("status = ?", 1).Order("id asc").Limit(batchSize)
	if lastID > 0 {
		query = query.Where("id > ?", lastID)
	}
	err := query.Find(&records).Error
	var result []*domain.Channel
	if records != nil {
		result = make([]*domain.Channel, len(records))
		for i, record := range records {
			result[i] = store.track(record)
		}
	}
	return result, err
}
func (Store) CountEnabled() (int64, error) {
	var count int64
	err := model.DB.Model(&model.Channel{}).Where("status = ?", 1).Count(&count).Error
	return count, err
}

func (store Store) NextEnabled(after int) ([]*domain.Channel, int, bool, error) {
	channels, err := store.EnabledAfter(after, 100)
	next := after
	if len(channels) > 0 {
		next = channels[len(channels)-1].Id
	}
	return channels, next, len(channels) < 100, err
}

func (store Store) RefreshCandidates(cursor int) ([]*domain.Channel, int, error) {
	var records []*model.Channel
	err := model.DB.Select("id", "name", "key", "status", "channel_info").Where("type = ? AND (status = ? OR status = ?)", constant.ChannelTypeCodex, 1, 3).Order("id asc").Limit(200).Offset(cursor).Find(&records).Error
	var channels []*domain.Channel
	if records != nil {
		channels = make([]*domain.Channel, len(records))
		for i, ch := range records {
			channels[i] = store.track(ch)
		}
	}
	return channels, cursor + 200, err
}
