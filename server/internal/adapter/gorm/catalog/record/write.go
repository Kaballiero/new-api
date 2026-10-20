package record

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"gorm.io/gorm"
)

type metadataWrite struct{ db *gorm.DB }

func (store Store) WithModelMutation(change func(domain.ModelMutation) error) error {
	return store.env.Transaction(func(tx *gorm.DB) error { return change(metadataWrite{db: tx}) })
}
func (store Store) WithVendorMetadata(change func(domain.VendorMetadataMutation) error) error {
	return store.env.Transaction(func(tx *gorm.DB) error { return change(metadataWrite{db: tx}) })
}
func (tx metadataWrite) VendorExists(id int) (bool, error) {
	var count int64
	err := tx.db.Model(&Vendor{}).Where("id = ?", id).Count(&count).Error
	return count != 0, err
}
func (tx metadataWrite) InsertModel(value *domain.Model) error {
	v := FromModel(value)
	defer func() { *value = *ToModel(v) }()
	return tx.db.Create(v).Error
}
func (tx metadataWrite) RestoreModelDefaults(id, status, sync int) error {
	return tx.db.Model(&Model{}).Where("id = ?", id).Updates(map[string]any{"status": status, "sync_official": sync}).Error
}
func (tx metadataWrite) UpdateModel(value *domain.Model) error {
	v := FromModel(value)
	defer func() { *value = *ToModel(v) }()
	return tx.db.Model(&Model{}).Where("id = ?", v.Id).Select("model_name", "description", "icon", "tags", "vendor_id", "endpoints", "status", "sync_official", "name_rule", "updated_time").Updates(v).Error
}
func (tx metadataWrite) SavedVendor(id int) (*domain.Vendor, error) {
	var v Vendor
	err := tx.db.First(&v, id).Error
	return ToVendor(&v), err
}
func (tx metadataWrite) OtherVendors(id int) ([]domain.Vendor, error) {
	var rows []Vendor
	err := tx.db.Select("id", "name").Where("id <> ?", id).Find(&rows).Error
	var out []domain.Vendor
	for i := range rows {
		out = append(out, *ToVendor(&rows[i]))
	}
	return out, err
}
func (tx metadataWrite) InsertVendor(value *domain.Vendor) error {
	v := FromVendor(value)
	defer func() { *value = *ToVendor(v) }()
	return tx.db.Create(v).Error
}
func (tx metadataWrite) UpdateVendor(value *domain.Vendor) error {
	v := FromVendor(value)
	defer func() { *value = *ToVendor(v) }()
	return tx.db.Model(&Vendor{}).Where("id = ?", v.Id).Updates(map[string]any{"name": v.Name, "description": v.Description, "icon": v.Icon, "updated_time": v.UpdatedTime}).Error
}
