package channels

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"gorm.io/gorm"
)

// The gateway settings of a channel (setting and settings columns) are encoded
// with the relaykit DTOs here, so channel scenarios see only domain values.

func (store Store) ValidateSettings(channel *domain.Channel) error {
	return store.MutableRecord(channel).ValidateSettings()
}
func (store Store) TaskPluginKey(channel *domain.Channel) string {
	record := store.MutableRecord(channel)
	key := NewChannelPersistence().GetSetting(record).TaskPluginKey
	*channel = *ToDomain(record)
	return key
}
func (store Store) VertexJSONKeys(channel *domain.Channel) bool {
	record := store.MutableRecord(channel)
	jsonKeys := NewChannelPersistence().GetOtherSettings(record).VertexKeyType != dto.VertexKeyTypeAPIKey
	*channel = *ToDomain(record)
	return jsonKeys
}
func (store Store) SetProxy(channel *domain.Channel, proxy string) {
	record := store.MutableRecord(channel)
	settings := NewChannelPersistence().GetSetting(record)
	*channel = *ToDomain(record)
	settings.Proxy = proxy
	record = store.MutableRecord(channel)
	record.SetSetting(settings)
	*channel = *ToDomain(record)
}
func (store Store) EditAdvancedCustom(channel *domain.Channel) func(*string) error {
	record := store.MutableRecord(channel)
	settings := NewChannelPersistence().GetOtherSettings(record)
	*channel = *ToDomain(record)
	return func(raw *string) error {
		if raw != nil {
			var config dto.AdvancedCustomConfig
			if err := common.UnmarshalJsonStr(*raw, &config); err != nil {
				return err
			}
			settings.AdvancedCustom = &config
		}
		record := store.MutableRecord(channel)
		record.SetOtherSettings(settings)
		*channel = *ToDomain(record)
		return nil
	}
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
	err := CanonicalizeModelConfig(record)
	*channel = *ToDomain(record)
	return err
}
func (store Store) Insert(channel *domain.Channel) error {
	record := store.MutableRecord(channel)
	err := NewChannelPersistence().Insert(record)
	*channel = *ToDomain(record)
	return err
}
func (store Store) InsertBatch(channels []domain.Channel) error {
	records := make([]legacy.Channel, len(channels))
	for i := range channels {
		records[i] = *FromDomain(&channels[i])
	}
	return BatchInsertChannels(records)
}

func (store Store) EditTag(tag string, newTag, modelMapping, models, groups *string, priority *int64, weight *uint, paramOverride, headerOverride *string) error {
	return EditChannelByTag(tag, newTag, modelMapping, models, groups, priority, weight, paramOverride, headerOverride)
}

func (store Store) WithLocked(id int, apply func(*domain.Channel, func(*domain.Channel) error) error) error {
	return legacy.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := GetChannelByIdForUpdate(tx, id)
		if err != nil {
			return err
		}
		return apply(store.track(locked), func(channel *domain.Channel) error {
			record := store.MutableRecord(channel)
			err := NewChannelPersistence().UpdateWithTx(record, tx)
			*channel = *ToDomain(record)
			return err
		})
	})
}

func (store Store) SetKey(id int, key string) error {
	return legacy.DB.Model(&legacy.Channel{}).Where("id = ?", id).Update("key", key).Error
}
func (store Store) InitCache() { InitChannelCache() }
