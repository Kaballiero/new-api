package delete_vendor

import "github.com/QuantumNous/new-api/internal/catalog/domain"

type Store interface {
	IsMissing(error) bool
	WithVendorMutation(func(domain.VendorMutation) error) error
	Vendor(int) (*domain.Vendor, error)
}
type Usecase struct {
	store    Store
	versions domain.OperationVersions
	now      func() int64
	refresh  func()
}

func NewUsecase(store Store, now func() int64, refresh func(), versions domain.OperationVersions) Usecase {
	return Usecase{store: store, now: now, refresh: refresh, versions: versions}
}
func (u Usecase) Execute(input Input) (*Output, error) {
	if _, err := u.store.Vendor(input.ID); err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
	}
	operation := domain.VendorOperation{Action: "delete", VendorIDs: []int{input.ID}}
	result := &domain.VendorOperationResult{UpdatedModels: []int{}, DeletedVendors: []int{}}
	err := u.store.WithVendorMutation(func(tx domain.VendorMutation) error {
		return domain.ApplyVendorOperation(tx, operation, result, u.now, u.versions)
	})
	if err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
	}
	u.refresh()
	return result, nil
}
