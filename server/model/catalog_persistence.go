package model

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"gorm.io/gorm"
)

func NewCatalogPersistence() record.Store {
	return record.NewStore(record.Environment{
		LockMetadata: func() func() { metadataMutationMu.Lock(); return metadataMutationMu.Unlock }, LockRows: lockForUpdate, PricingMutation: mutateModelPricingOptions, LockMetadataRow: lockMetadataMutation,
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
		DB: func() *gorm.DB { return DB }, Transaction: metadataTransaction})
}

type catalogVendorOperationBridge struct{}

func (catalogVendorOperationBridge) IsMissing(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
func (catalogVendorOperationBridge) Vendors() ([]domain.Vendor, error) {
	return NewCatalogPersistence().OperationVendors()
}
func (catalogVendorOperationBridge) Models(action string, ids []int) ([]domain.Model, error) {
	return NewCatalogPersistence().OperationModels(action, ids)
}
func (catalogVendorOperationBridge) WithVendorMutation(change func(domain.VendorMutation) error) error {
	return NewCatalogPersistence().WithVendorMutation(change)
}

type catalogMetadataSyncBridge struct{}

func (catalogMetadataSyncBridge) SyncFields() []string { return MetadataSyncFields }
func (catalogMetadataSyncBridge) WithMetadataSync(change func(domain.MetadataMutation) error) error {
	return NewCatalogPersistence().WithMetadataSync(change)
}

type catalogReadBridge struct{}

func (catalogReadBridge) Models(keyword, vendor, status, sync string, offset, limit int) ([]*domain.Model, int64, error) {
	rows, n, err := NewCatalogPersistence().SearchModels(keyword, vendor, status, sync, offset, limit)
	return record.ModelsToDomain(rows), n, err
}
func (catalogReadBridge) ConfiguredChannels() (map[string][]int, error) {
	return NewCatalogPersistence().ConfiguredChannels()
}
func (catalogReadBridge) ExactNames() ([]string, error) { return NewCatalogPersistence().ExactNames() }
func (catalogReadBridge) EnabledModels() []string       { return GetEnabledModels() }
func (catalogReadBridge) ExistingNames(names []string) ([]string, error) {
	return NewCatalogPersistence().ExistingNames(names)
}
func catalogConnectionValue(v ModelConnection) domain.ModelConnection {
	return domain.ModelConnection{Group: v.Group, Model: v.Model, ChannelId: v.ChannelId, Enabled: v.Enabled, Priority: v.Priority, Weight: v.Weight, Tag: v.Tag, ChannelType: v.ChannelType, ChannelName: v.ChannelName}
}

type catalogMetadataWriteBridge struct{}

func (catalogMetadataWriteBridge) WithModelMutation(change func(domain.ModelMutation) error) error {
	return NewCatalogPersistence().WithModelMutation(change)
}
func (catalogMetadataWriteBridge) WithVendorMetadata(change func(domain.VendorMetadataMutation) error) error {
	return NewCatalogPersistence().WithVendorMetadata(change)
}
