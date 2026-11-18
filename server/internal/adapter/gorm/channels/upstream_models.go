package channels

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	legacy "github.com/QuantumNous/new-api/model"
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
func (store Store) ModelUpdateState(channel *domain.Channel) (domain.ModelUpdateState, error) {
	settings, err := parseOtherSettings(channel)
	if err != nil {
		return domain.ModelUpdateState{}, err
	}
	return domain.ModelUpdateState{
		CheckEnabled:        settings.UpstreamModelUpdateCheckEnabled,
		AutoSyncEnabled:     settings.UpstreamModelUpdateAutoSyncEnabled,
		AutoRemoveEnabled:   settings.UpstreamModelUpdateAutoRemoveEnabled,
		CatalogSyncEnabled:  settings.UpstreamModelUpdateCatalogSyncEnabled,
		LastCheckTime:       settings.UpstreamModelUpdateLastCheckTime,
		LastDetectedModels:  settings.UpstreamModelUpdateLastDetectedModels,
		LastRemovedModels:   settings.UpstreamModelUpdateLastRemovedModels,
		LastConflictModels:  settings.UpstreamModelUpdateLastConflictModels,
		IgnoredModels:       settings.UpstreamModelUpdateIgnoredModels,
		ManualReserveModels: store.manualReserveModels(channel),
	}, nil
}

// invalidReserveSettings keeps, by channel ID, the invalid setting whose
// manual reserves were last reported unreadable, so it is logged once.
var invalidReserveSettings sync.Map

// manualReserveModels are the upstream models a manual reserve of an
// OpenRouter channel covers: the models of its exact reserves and the Sonar
// research model of its legacy reserve, each above zero. An unreadable setting
// covers none; it does not fail the state, so a channel whose checks are off
// is not reported as failed, and a check still fails on it (FetchFingerprint).
func (Store) manualReserveModels(channel *domain.Channel) []string {
	if channel.Type != constant.ChannelTypeOpenRouter {
		return nil
	}
	setting, err := parseChannelSetting(channel)
	if err != nil {
		if previous, logged := invalidReserveSettings.Swap(channel.Id, *channel.Setting); !logged || previous != *channel.Setting {
			common.SysLog(fmt.Sprintf("channel manual reserves unreadable, none applied: channel_id=%d err=%v", channel.Id, err))
		}
		return nil
	}
	// The legacy reserve covers the Sonar research model unless an exact
	// model reserve takes precedence (OpenRouterReserveUSD).
	models := slices.Sorted(maps.Keys(setting.OpenRouterModelReserveUSD))
	if !slices.Contains(models, dto.OpenRouterSonarResearchModel) {
		models = append(models, dto.OpenRouterSonarResearchModel)
	}
	var covered []string
	for _, model := range models {
		if reserve := setting.OpenRouterReserveUSD(model); reserve != nil && *reserve > 0 {
			covered = append(covered, model)
		}
	}
	return covered
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

func parseChannelSetting(channel *domain.Channel) (dto.ChannelSettings, error) {
	setting := dto.ChannelSettings{}
	if channel.Setting == nil || strings.TrimSpace(*channel.Setting) == "" {
		return setting, nil
	}
	if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
		return dto.ChannelSettings{}, fmt.Errorf("invalid channel setting: %w", err)
	}
	return setting, nil
}

// FetchFingerprint returns the configuration that decides where and how the
// upstream models of the channel are fetched.
func (store Store) FetchFingerprint(channel *domain.Channel) (domain.ModelFetchFingerprint, error) {
	if channel == nil {
		return domain.ModelFetchFingerprint{}, fmt.Errorf("channel is nil")
	}

	setting, err := parseChannelSetting(channel)
	if err != nil {
		return domain.ModelFetchFingerprint{}, err
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
	otherSettings := NewChannelPersistence().GetOtherSettings(record)
	fingerprint.AdvancedCustom = otherSettings.AdvancedCustom
	fingerprint.CatalogSyncEnabled = otherSettings.UpstreamModelUpdateCatalogSyncEnabled
	*channel = *ToDomain(record)
	return fingerprint, nil
}

// WithLockedModelState applies the upstream model update state over the
// other settings of the locked channel.
func (store Store) WithLockedModelState(id int, apply func(*domain.Channel, func(domain.ModelUpdateState, bool) error) error) error {
	return legacy.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := GetChannelByIdForUpdate(tx, id)
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
			settings.UpstreamModelUpdateCatalogSyncEnabled = state.CatalogSyncEnabled
			settings.UpstreamModelUpdateLastCheckTime = state.LastCheckTime
			settings.UpstreamModelUpdateLastDetectedModels = state.LastDetectedModels
			settings.UpstreamModelUpdateLastRemovedModels = state.LastRemovedModels
			settings.UpstreamModelUpdateLastConflictModels = state.LastConflictModels
			settings.UpstreamModelUpdateIgnoredModels = state.IgnoredModels
			record := store.MutableRecord(channel)
			record.SetOtherSettings(settings)
			*channel = *ToDomain(record)
			updates := map[string]any{"settings": record.OtherSettings}
			if changed {
				updates["models"] = record.Models
				updates["model_mapping"] = record.ModelMapping
			}
			if err := tx.Model(&legacy.Channel{}).Where("id = ?", record.Id).Updates(updates).Error; err != nil {
				return err
			}
			if changed {
				return UpdateAbilities(record, tx)
			}
			return nil
		})
	})
}
func (store Store) EnabledAfter(lastID, batchSize int) ([]*domain.Channel, error) {
	var records []*legacy.Channel
	query := legacy.DB.Select(channelUpstreamModelUpdateSelectFields).Where("status = ?", 1).Order("id asc").Limit(batchSize)
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
	err := legacy.DB.Model(&legacy.Channel{}).Where("status = ?", 1).Count(&count).Error
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
	var records []*legacy.Channel
	err := legacy.DB.Select("id", "name", "key", "status", "channel_info").Where("type = ? AND (status = ? OR status = ?)", constant.ChannelTypeCodex, 1, 3).Order("id asc").Limit(200).Offset(cursor).Find(&records).Error
	var channels []*domain.Channel
	if records != nil {
		channels = make([]*domain.Channel, len(records))
		for i, ch := range records {
			channels[i] = store.track(ch)
		}
	}
	return channels, cursor + 200, err
}
