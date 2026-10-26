package model

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"gorm.io/gorm"
)

// BRIDGE: NewCatalogPersistence shares the metadata transaction and private pricing mutation with app/catalog; remove after pricing composition has its owner.
func NewCatalogPersistence() record.Store {
	return record.NewStore(record.Environment{
		LockMetadata: func() func() { metadataMutationMu.Lock(); return metadataMutationMu.Unlock }, LockRows: LockForUpdate, PricingMutation: mutateModelPricingOptions, LockMetadataRow: lockMetadataMutation,
		ReadRouting: func(tx *gorm.DB) ([]domain.ChannelRouting, error) {
			var rows []Channel
			err := tx.Select("id", "models", "status", "group", "priority", "weight", "tag").Order("id").Find(&rows).Error
			var out []domain.ChannelRouting
			for _, v := range rows {
				out = append(out, domain.ChannelRouting{ID: v.Id, Models: v.Models, Status: v.Status, Group: v.Group, Priority: v.Priority, Weight: v.Weight, Tag: v.Tag})
			}
			return out, err
		},
		SaveRouting: func(tx *gorm.DB, v domain.ChannelRouting) error {
			channel := Channel{Id: v.ID, Models: v.Models, Status: v.Status, Group: v.Group, Priority: v.Priority, Weight: v.Weight, Tag: v.Tag}
			if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Update("models", channel.Models).Error; err != nil {
				return err
			}
			return channel.UpdateAbilities(tx)
		},
		GroupColumn: func() string { return commonGroupCol }, DB: func() *gorm.DB { return DB }, Transaction: metadataTransaction})
}
