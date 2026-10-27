package catalog

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	legacy "github.com/QuantumNous/new-api/model"
)

func NewCatalogPersistence(pricingMutation func(func(*gorm.DB, map[string]map[string]any) error) error) record.Store {
	return record.NewStore(record.Environment{

		LockMetadata:    func() func() { MetadataMutationMu.Lock(); return MetadataMutationMu.Unlock },
		LockRows:        locking.LockForUpdate,
		PricingMutation: pricingMutation,
		LockMetadataRow: LockMetadataMutation,

		ReadRouting: func(tx *gorm.DB) ([]domain.ChannelRouting, error) {
			var rows []legacy.Channel
			err := tx.Select("id", "models", "status", "group", "priority", "weight", "tag").Order("id").Find(&rows).Error
			var out []domain.ChannelRouting
			for _, v := range rows {
				out = append(
					out,
					domain.ChannelRouting{
						ID:       v.Id,
						Models:   v.Models,
						Status:   v.Status,
						Group:    v.Group,
						Priority: v.Priority,
						Weight:   v.Weight,
						Tag:      v.Tag,
					},
				)
			}
			return out, err
		},

		SaveRouting: func(tx *gorm.DB, v domain.ChannelRouting) error {
			channel := legacy.Channel{Id: v.ID, Models: v.Models, Status: v.Status, Group: v.Group, Priority: v.Priority, Weight: v.Weight, Tag: v.Tag}
			if err := tx.Model(&legacy.Channel{}).Where("id = ?", channel.Id).Update("models", channel.Models).Error; err != nil {
				return err
			}
			return channelstore.UpdateAbilities(&(channel), tx)
		},

		GroupColumn: func() string { return legacy.GroupColumn },
		DB:          func() *gorm.DB { return legacy.DB },
		Transaction: MetadataTransaction,
	})
}
