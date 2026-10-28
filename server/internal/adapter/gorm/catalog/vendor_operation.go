package catalog

import "github.com/QuantumNous/new-api/internal/catalog/domain"

type VendorOperationStore struct{ Store }

func NewVendorOperationStore(store Store) VendorOperationStore {
	return VendorOperationStore{Store: store}
}
func (store VendorOperationStore) Vendors() ([]domain.Vendor, error) {
	return store.persistence.OperationVendors()
}
func (store VendorOperationStore) Models(action string, ids []int) ([]domain.Model, error) {
	return store.persistence.OperationModels(action, ids)
}
func (store Store) WithVendorMutation(change func(domain.VendorMutation) error) error {
	return store.persistence.WithVendorMutation(change)
}
