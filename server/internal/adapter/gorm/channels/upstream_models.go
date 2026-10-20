package channels

import (
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

func (store Store) WithLockedModelState(id int, apply func(*domain.Channel, func(dto.ChannelOtherSettings, bool) error) error) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := model.GetChannelByIdForUpdate(tx, id)
		if err != nil {
			return err
		}
		channel := store.track(locked)
		return apply(channel, func(settings dto.ChannelOtherSettings, changed bool) error {
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
