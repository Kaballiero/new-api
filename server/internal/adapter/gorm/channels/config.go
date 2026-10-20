package channels

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"gorm.io/gorm"
)

func (store Store) ValidateSettings(channel *domain.Channel) error {
	return store.MutableRecord(channel).ValidateSettings()
}
func (store Store) Settings(channel *domain.Channel) dto.ChannelSettings {
	record := store.MutableRecord(channel)
	settings := record.GetSetting()
	*channel = *ToDomain(record)
	return settings
}
func (store Store) OtherSettings(channel *domain.Channel) dto.ChannelOtherSettings {
	record := store.MutableRecord(channel)
	settings := record.GetOtherSettings()
	*channel = *ToDomain(record)
	return settings
}
func (store Store) PluginBaseURL(key string) (string, bool) {
	plugin, ok := jsplugin.DefaultRegistry.Get(key)
	if !ok {
		return "", false
	}
	return plugin.Meta.BaseURL, true
}
func (store Store) Canonicalize(channel *domain.Channel) error {
	record := store.MutableRecord(channel)
	err := record.CanonicalizeModelConfig()
	*channel = *ToDomain(record)
	return err
}
func (store Store) Insert(channel *domain.Channel) error {
	record := store.MutableRecord(channel)
	err := record.Insert()
	*channel = *ToDomain(record)
	return err
}
func (store Store) InsertBatch(channels []domain.Channel) error {
	records := make([]model.Channel, len(channels))
	for i := range channels {
		records[i] = *FromDomain(&channels[i])
	}
	return model.BatchInsertChannels(records)
}

func (store Store) EditTag(tag string, newTag, modelMapping, models, groups *string, priority *int64, weight *uint, paramOverride, headerOverride *string) error {
	return model.EditChannelByTag(tag, newTag, modelMapping, models, groups, priority, weight, paramOverride, headerOverride)
}

func (store Store) WithLocked(id int, apply func(*domain.Channel, func(*domain.Channel) error) error) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := model.GetChannelByIdForUpdate(tx, id)
		if err != nil {
			return err
		}
		return apply(store.track(locked), func(channel *domain.Channel) error {
			record := store.MutableRecord(channel)
			err := record.UpdateWithTx(tx)
			*channel = *ToDomain(record)
			return err
		})
	})
}

func (store Store) SetKey(id int, key string) error {
	return model.DB.Model(&model.Channel{}).Where("id = ?", id).Update("key", key).Error
}
func (store Store) InitCache() { model.InitChannelCache() }

func (store Store) SetOtherSettings(channel *domain.Channel, settings dto.ChannelOtherSettings) {
	record := store.MutableRecord(channel)
	record.SetOtherSettings(settings)
	*channel = *ToDomain(record)
}
func (store Store) SetSettings(channel *domain.Channel, settings dto.ChannelSettings) {
	record := store.MutableRecord(channel)
	record.SetSetting(settings)
	*channel = *ToDomain(record)
}
