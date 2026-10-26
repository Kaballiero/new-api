package record

import (
	"github.com/QuantumNous/new-api/internal/catalog/apply_metadata_sync"

	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"gorm.io/gorm"
)

type syncMutation struct {
	locked bool
	store  Store
	db     *gorm.DB
}

func (store Store) WithMetadataSync(change func(apply_metadata_sync.MetadataMutation) error) error {
	return store.env.Transaction(func(tx *gorm.DB) error { return change(syncMutation{store: store, db: tx, locked: true}) })
}
func (store Store) SyncState() (map[string]*domain.Model, map[string]*domain.Vendor, error) {
	return (syncMutation{store: store, db: store.env.DB()}).State()
}
func (tx syncMutation) State() (map[string]*domain.Model, map[string]*domain.Vendor, error) {
	var models []*Model
	var vendors []*Vendor
	q := tx.db
	if tx.locked {
		q = tx.store.env.LockRows(q)
	}
	if err := q.Session(&gorm.Session{}).Order("id").Find(&models).Error; err != nil {
		return nil, nil, err
	}
	if err := q.Session(&gorm.Session{}).Order("id").Find(&vendors).Error; err != nil {
		return nil, nil, err
	}
	mm := make(map[string]*domain.Model, len(models))
	vv := make(map[string]*domain.Vendor, len(vendors))
	for _, v := range models {
		mm[v.ModelName] = ToModel(v)
	}
	for _, v := range vendors {
		vv[v.Name] = ToVendor(v)
	}
	return mm, vv, nil
}
func (tx syncMutation) InsertVendor(value *domain.Vendor) error {
	return (metadataWrite{db: tx.db}).InsertVendor(value)
}
func (tx syncMutation) OtherVendors(id int) ([]domain.Vendor, error) {
	return (metadataWrite{db: tx.db}).OtherVendors(id)
}

func (tx syncMutation) InsertModel(fields map[string]any) error {
	return tx.db.Model(&Model{}).Create(fields).Error
}
func (tx syncMutation) UpdateModel(id int, fields map[string]any) error {
	return tx.db.Model(&Model{}).Where("id = ?", id).Updates(fields).Error
}
