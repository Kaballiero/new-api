package record

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"

	"gorm.io/gorm"
)

type vendorMutation struct {
	db     *gorm.DB
	locked bool
	store  Store
}

func (store Store) WithVendorMutation(change func(domain.VendorMutation) error) error {
	return store.env.Transaction(func(tx *gorm.DB) error { return change(vendorMutation{db: tx, locked: true, store: store}) })
}
func (tx vendorMutation) Vendors() ([]domain.Vendor, error) {
	var rows []Vendor
	err := tx.query().Session(&gorm.Session{}).Order("id").Find(&rows).Error
	var out []domain.Vendor
	for i := range rows {
		out = append(out, *ToVendor(&rows[i]))
	}
	return out, err
}
func (tx vendorMutation) Models(action string, ids []int) ([]domain.Model, error) {
	var rows []Model
	q := tx.query().Session(&gorm.Session{}).Model(&Model{}).Order("id")
	if action == "assign" {
		q = q.Where("id IN ?", ids)
	} else {
		q = q.Where("vendor_id IN ?", ids)
	}
	err := q.Find(&rows).Error
	var out []domain.Model
	for i := range rows {
		out = append(out, *ToModel(&rows[i]))
	}
	return out, err
}
func (tx vendorMutation) Assign(ids []int, target int, now int64) error {
	return tx.db.Session(&gorm.Session{}).Model(&Model{}).Where("id IN ?", ids).Updates(map[string]any{"vendor_id": target, "updated_time": now}).Error
}
func (tx vendorMutation) Delete(ids []int) error {
	return tx.db.Session(&gorm.Session{}).Where("id IN ?", ids).Delete(&Vendor{}).Error
}
func (store Store) OperationVendors() ([]domain.Vendor, error) {
	return (vendorMutation{db: store.env.DB()}).Vendors()
}
func (store Store) OperationModels(action string, ids []int) ([]domain.Model, error) {
	return (vendorMutation{db: store.env.DB()}).Models(action, ids)
}

func (tx vendorMutation) query() *gorm.DB {
	if tx.locked {
		return tx.store.env.LockRows(tx.db)
	}
	return tx.db
}
