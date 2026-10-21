package catalog

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"gorm.io/gorm"
)

type Runtime struct {
	SyncFields       func() []string
	EnabledModels    func() []string
	SupportEndpoints func(string) []string
	QuotaTypes       func(string) []int
}
type Store struct {
	persistence record.Store
	runtime     Runtime
}

func NewStore(persistence record.Store, runtime Runtime) Store {
	return Store{persistence: persistence, runtime: runtime}
}
func (Store) IsMissing(err error) bool   { return errors.Is(err, gorm.ErrRecordNotFound) }
func (store Store) SyncFields() []string { return store.runtime.SyncFields() }
func (store Store) Models(keyword, vendor, status, sync string, offset, limit int) ([]*domain.Model, int64, error) {
	v, n, e := store.persistence.SearchModels(keyword, vendor, status, sync, offset, limit)
	return record.ModelsToDomain(v), n, e
}
func (store Store) Model(id int) (*domain.Model, error) {
	v, e := store.persistence.Model(id)
	return record.ToModel(v), e
}
func (store Store) Vendor(id int) (*domain.Vendor, error) {
	v, e := store.persistence.GetVendorByID(id)
	return record.ToVendor(v), e
}
func (store Store) Vendors(keyword string, offset, limit int, association string) ([]*domain.Vendor, int64, error) {
	v, n, e := store.persistence.SearchVendors(keyword, offset, limit, association)
	return record.VendorsToDomain(v), n, e
}
func (store Store) ModelDuplicate(id int, name string) (bool, error) {
	return store.persistence.IsModelNameDuplicated(id, name)
}
func (store Store) VendorDuplicate(id int, name string) (bool, error) {
	return store.persistence.IsVendorNameDuplicated(id, name)
}
func (store Store) VendorCounts() (map[int64]int64, error) {
	return store.persistence.GetVendorModelCounts()
}
func (store Store) ConfiguredChannels() (map[string][]int, error) {
	return store.persistence.ConfiguredChannels()
}
func (store Store) Connections() ([]domain.ModelConnection, error) {
	return store.persistence.Connections()
}
func (store Store) AllModels() ([]domain.Model, error) { return store.persistence.AllModelValues() }
func (store Store) ExactNames() ([]string, error)      { return store.persistence.ExactNames() }
func (store Store) SupportEndpoints(name string) []string {
	return store.runtime.SupportEndpoints(name)
}
func (store Store) QuotaTypes(name string) []int { return store.runtime.QuotaTypes(name) }
func (store Store) SetModelStatus(id, status int) error {
	return store.persistence.SetModelStatus(id, status)
}
func (store Store) LockMetadata() func() { return store.persistence.LockMetadata() }
func (store Store) WithDeletion(pricing bool, change func(domain.MetadataDeletion) error) error {
	return store.persistence.WithDeletion(pricing, change)
}
func (store Store) ModelExists(id int) error { return store.persistence.ModelExists(id) }
func (store Store) WithMetadataSync(change func(domain.MetadataMutation) error) error {
	return store.persistence.WithMetadataSync(change)
}
func (store Store) SyncState() (map[string]*domain.Model, map[string]*domain.Vendor, error) {
	return store.persistence.SyncState()
}
func (store Store) EnabledModels() []string { return store.runtime.EnabledModels() }
func (store Store) ExistingNames(names []string) ([]string, error) {
	return store.persistence.ExistingNames(names)
}
func (store Store) WithModelMutation(change func(domain.ModelMutation) error) error {
	return store.persistence.WithModelMutation(change)
}
func (store Store) WithVendorMetadata(change func(domain.VendorMetadataMutation) error) error {
	return store.persistence.WithVendorMetadata(change)
}
