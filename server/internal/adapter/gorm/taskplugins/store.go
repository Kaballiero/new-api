package taskplugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/taskplugins/record"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskPlugin = record.TaskPlugin
type Store struct{ db func() *gorm.DB }

func NewStore(db func() *gorm.DB) Store { return Store{db: db} }
func pluginValues(rows []TaskPlugin) []domain.Plugin {
	if rows == nil {
		return nil
	}
	out := make([]domain.Plugin, len(rows))
	for i := range rows {
		out[i] = domain.Plugin(rows[i])
	}
	return out
}
func lookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrNotFound
	}
	return err
}
func (s Store) SaveTaskPlugin(value *domain.Plugin) error {
	plugin := (*TaskPlugin)(value)
	return s.db().Transaction(func(tx *gorm.DB) error {
		var existing TaskPlugin
		err := tx.Where(&TaskPlugin{Key: plugin.Key, Version: plugin.Version}).First(&existing).Error
		if err == nil {
			if existing.SourceHash != plugin.SourceHash {
				return domain.ErrSourceConflict
			}
			updates := map[string]any{"enabled": plugin.Enabled, "remark": plugin.Remark}
			if plugin.Icon != "" {
				updates["icon"] = plugin.Icon
				existing.Icon = plugin.Icon
			}
			if err = tx.Model(&existing).Updates(updates).Error; err != nil {
				return err
			}
			existing.Enabled = plugin.Enabled
			existing.Remark = plugin.Remark
			*plugin = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		plugin.CreatedAt = time.Now().Unix()
		var count int64
		if err = tx.Model(&TaskPlugin{}).Where(&TaskPlugin{Key: plugin.Key, Active: true}).Count(&count).Error; err != nil {
			return err
		}
		plugin.Active = count == 0
		return tx.Create(plugin).Error
	})
}

func (s Store) ListTaskPluginVersions(key string) ([]domain.Plugin, error) {
	var plugins []TaskPlugin
	err := s.db().Where(&TaskPlugin{Key: key}).Order("created_at DESC, id DESC").Find(&plugins).Error
	return pluginValues(plugins), err
}

func (s Store) ListTaskPlugins() ([]domain.Plugin, error) {
	var plugins []TaskPlugin
	err := s.db().
		Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "created_at"}, Desc: true}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}, Desc: true}).
		Find(&plugins).Error
	return pluginValues(plugins), err
}

func (s Store) GetTaskPluginVersion(key, version string) (*domain.Plugin, error) {
	var plugin TaskPlugin
	query := s.db().Where(&TaskPlugin{Key: key})
	if version == "" {
		query = query.Where(&TaskPlugin{Active: true})
	} else {
		query = query.Where(&TaskPlugin{Version: version})
	}
	if err := query.First(&plugin).Error; err != nil {
		return nil, lookupError(err)
	}
	return (*domain.Plugin)(&plugin), nil
}

func (s Store) GetTaskPluginSyncSnapshot() (domain.SyncSnapshot, error) {
	var activePlugins []TaskPlugin
	if err := s.db().Where(&TaskPlugin{Active: true}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "version"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Find(&activePlugins).Error; err != nil {
		return domain.SyncSnapshot{}, err
	}

	type revisionEntry struct {
		Key        string `json:"key"`
		APIVersion int    `json:"api_version"`
		Version    string `json:"version"`
		SourceHash string `json:"source_hash"`
		Enabled    bool   `json:"enabled"`
	}
	entries := make([]revisionEntry, 0, len(activePlugins))
	enabledPlugins := make([]TaskPlugin, 0, len(activePlugins))
	for _, plugin := range activePlugins {
		entries = append(entries, revisionEntry{
			Key:        plugin.Key,
			APIVersion: plugin.APIVersion,
			Version:    plugin.Version,
			SourceHash: plugin.SourceHash,
			Enabled:    plugin.Enabled,
		})
		if plugin.Enabled {
			enabledPlugins = append(enabledPlugins, plugin)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Key != entries[j].Key {
			return entries[i].Key < entries[j].Key
		}
		return entries[i].Version < entries[j].Version
	})
	payload, err := common.Marshal(entries)
	if err != nil {
		return domain.SyncSnapshot{}, err
	}
	digest := sha256.Sum256(payload)
	return domain.SyncSnapshot{
		Plugins:  pluginValues(enabledPlugins),
		Revision: hex.EncodeToString(digest[:]),
	}, nil
}

func (s Store) ActivateTaskPlugin(key, version string) error {
	err := s.db().Transaction(func(tx *gorm.DB) error {
		var target TaskPlugin
		if err := tx.Where(&TaskPlugin{Key: key, Version: version}).First(&target).Error; err != nil {
			return err
		}
		if err := tx.Model(&TaskPlugin{}).Where(&TaskPlugin{Key: key}).Update("active", false).Error; err != nil {
			return err
		}
		return tx.Model(&target).Updates(map[string]any{"active": true, "enabled": true}).Error
	})
	return lookupError(err)
}

func (s Store) SetTaskPluginEnabled(key string, enabled bool) error {
	result := s.db().Model(&TaskPlugin{}).Where(&TaskPlugin{Key: key, Active: true}).Update("enabled", enabled)
	if result.Error != nil {
		return lookupError(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s Store) DeleteTaskPluginVersion(key, version string) (domain.DeleteResult, error) {
	result := domain.DeleteResult{}
	err := s.db().Transaction(func(tx *gorm.DB) error {
		var plugin TaskPlugin
		if err := model.LockForUpdate(tx).Where(&TaskPlugin{Key: key, Version: version}).First(&plugin).Error; err != nil {
			return err
		}
		result.DeletedActive = plugin.Active
		if err := tx.Delete(&plugin).Error; err != nil {
			return err
		}
		if !plugin.Active {
			return nil
		}

		var promoted TaskPlugin
		err := model.LockForUpdate(tx).
			Where(&TaskPlugin{Key: key}).
			Order("created_at DESC, id DESC").
			First(&promoted).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = tx.Model(&promoted).Update("active", true).Error; err != nil {
			return err
		}
		promoted.Active = true
		result.Promoted = (*domain.Plugin)(&promoted)
		return nil
	})
	return result, lookupError(err)
}
func (s Store) GetTaskPluginUsage(key string) ([]domain.ChannelRef, int64, error) {
	var channels []model.Channel
	if err := s.db().Where("type = ? AND status = ?", constant.ChannelTypeTaskPlugin, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, 0, err
	}
	refs := make([]domain.ChannelRef, 0)
	for _, channel := range channels {
		if channelstore.NewChannelPersistence().GetSetting(&(channel)).TaskPluginKey == key {
			refs = append(refs, domain.ChannelRef{Id: channel.Id, Name: channel.Name})
		}
	}
	var inFlight int64
	err := s.db().Model(&model.Task{}).Where("platform = ? AND status NOT IN ?",
		key,
		[]model.TaskStatus{model.TaskStatusSuccess,
			model.TaskStatusFailure}).Count(&inFlight).Error
	return refs, inFlight, err
}
