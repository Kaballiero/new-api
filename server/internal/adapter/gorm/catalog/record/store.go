package record

import (
	"strconv"

	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"gorm.io/gorm"
)

type Environment struct {
	LockMetadata    func() func()
	LockRows        func(*gorm.DB) *gorm.DB
	PricingMutation func(func(*gorm.DB, map[string]map[string]any) error) error
	LockMetadataRow func(*gorm.DB) error
	ReadRouting     func(*gorm.DB) ([]domain.ChannelRouting, error)
	SaveRouting     func(*gorm.DB, domain.ChannelRouting) error

	GroupColumn func() string
	DB          func() *gorm.DB
	Transaction func(func(*gorm.DB) error) error
}
type Store struct{ env Environment }

func NewStore(env Environment) Store    { return Store{env: env} }
func (store Store) GroupColumn() string { return store.env.GroupColumn() }
func (store Store) IsModelNameDuplicated(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := store.env.DB().Model(&Model{}).Where("model_name = ? AND id <> ?", name, id).Count(&cnt).Error
	return cnt > 0, err
}

func (store Store) SearchModels(keyword string, vendor string, status string, syncOfficial string, offset int, limit int) ([]*Model, int64, error) {
	var models []*Model
	db := store.env.DB().Model(&Model{})
	if keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("model_name LIKE ? OR description LIKE ? OR tags LIKE ?", like, like, like)
	}
	if vendor != "" {
		if vid, err := strconv.Atoi(vendor); err == nil {
			db = db.Where("models.vendor_id = ?", vid)
		} else {
			db = db.Joins("JOIN vendors ON vendors.id = models.vendor_id").Where("vendors.name LIKE ?", "%"+vendor+"%")
		}
	}
	if statusValue, ok := domain.ParseModelStatusFilter(status); ok {
		db = db.Where("models.status = ?", statusValue)
	}
	if syncValue, ok := domain.ParseModelSyncFilter(syncOfficial); ok {
		db = db.Where("models.sync_official = ?", syncValue)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := db.Order("models.id DESC").Offset(offset).Limit(limit).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	return models, total, nil
}

func (store Store) GetVendorModelCounts() (map[int64]int64, error) {
	var stats []struct {
		VendorID int64
		Count    int64
	}
	if err := store.env.DB().Model(&Model{}).
		Select("vendor_id as vendor_id, count(*) as count").
		Group("vendor_id").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(stats))
	for _, s := range stats {
		m[s.VendorID] = s.Count
	}
	return m, nil
}

func (store Store) IsVendorNameDuplicated(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := store.env.DB().Model(&Vendor{}).Where("name = ? AND id <> ?", name, id).Count(&cnt).Error
	return cnt > 0, err
}

func (store Store) GetVendorByID(id int) (*Vendor, error) {
	var v Vendor
	err := store.env.DB().First(&v, id).Error
	if err != nil {
		return nil, err
	}
	if err := store.env.DB().Model(&Model{}).Where("vendor_id = ?", id).Count(&v.ModelCount).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

func (store Store) SearchVendors(keyword string, offset, limit int, association ...string) ([]*Vendor, int64, error) {
	db := store.env.DB().Model(&Vendor{})
	if keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	if len(association) > 0 {
		references := store.env.DB().Model(&Model{}).Select("1").Where("models.vendor_id = vendors.id")
		switch association[0] {
		case "linked":
			db = db.Where("EXISTS (?)", references)
		case "unlinked":
			db = db.Where("NOT EXISTS (?)", references)
		}
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var vendors []*Vendor
	if err := db.Offset(offset).Limit(limit).Order("id DESC").Find(&vendors).Error; err != nil {
		return nil, 0, err
	}
	counts, err := store.GetVendorModelCounts()
	if err != nil {
		return nil, 0, err
	}
	for _, vendor := range vendors {
		vendor.ModelCount = counts[int64(vendor.Id)]
	}
	return vendors, total, nil
}

func (store Store) LockMetadata() func() { return store.env.LockMetadata() }
func (store Store) WithDeletion(pricing bool, change func(domain.MetadataDeletion) error) error {
	if pricing {
		return store.env.PricingMutation(func(tx *gorm.DB, values map[string]map[string]any) error {
			if err := store.env.LockMetadataRow(tx); err != nil {
				return err
			}
			return change(deletion{store: store, db: tx, prices: values})
		})
	}
	return store.env.Transaction(func(tx *gorm.DB) error { return change(deletion{store: store, db: tx}) })
}

type deletion struct {
	store  Store
	db     *gorm.DB
	prices map[string]map[string]any
}

func (d deletion) ReadModels(ids []int) ([]domain.Model, error) {
	var rows []Model
	err := d.store.env.LockRows(d.db).Where("id IN ?", ids).Order("id").Find(&rows).Error
	var out []domain.Model
	for i := range rows {
		out = append(out, *ToModel(&rows[i]))
	}
	return out, err
}
func (d deletion) RoutingChannels() ([]domain.ChannelRouting, error) {
	return d.store.env.ReadRouting(d.store.env.LockRows(d.db))
}
func (d deletion) SaveRouting(row domain.ChannelRouting) error {
	return d.store.env.SaveRouting(d.db, row)
}
func (d deletion) DeleteModels(ids []int) error {
	return d.db.Where("id IN ?", ids).Delete(&Model{}).Error
}
func (d deletion) RemovePrices(names map[string]struct{}) {
	for _, entries := range d.prices {
		for name := range names {
			delete(entries, name)
		}
	}
}
