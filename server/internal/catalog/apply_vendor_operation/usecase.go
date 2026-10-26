package apply_vendor_operation

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	IsMissing(error) bool
	WithVendorMutation(func(domain.VendorMutation) error) error
}
type Usecase struct {
	store   Store
	now     func() int64
	refresh func()
}

func NewUsecase(store Store, now func() int64, refresh func()) Usecase {
	return Usecase{store: store, now: now, refresh: refresh}
}
func (u Usecase) Execute(input Input) (*Output, error) {
	operation := input
	if operation.ExpectedVersion == "" {
		return nil, domain.RejectVendorChange(domain.ErrVendorConflict, false)
	}
	result := &domain.VendorOperationResult{UpdatedModels: []int{}, DeletedVendors: []int{}}
	err := u.store.WithVendorMutation(func(tx domain.VendorMutation) error {
		return domain.ApplyVendorOperation(tx, operation, result, u.now)
	})
	if err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
	}
	u.refresh()
	return result, nil
}
