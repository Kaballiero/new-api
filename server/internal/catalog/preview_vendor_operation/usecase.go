package preview_vendor_operation

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	VendorConflict() error
	IsMissing(error) bool
	Vendors() ([]domain.Vendor, error)
	Models(string, []int) ([]domain.Model, error)
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute(input Input) (*Output, error) {
	operation := input
	plan, err := domain.NewVendorOperationPlan(operation)
	if err != nil {
		return nil, err
	}
	vendors, err := u.store.Vendors()
	if err != nil {
		return nil, err
	}
	if err := plan.SetVendors(vendors, u.store.VendorConflict); err != nil {
		return nil, err
	}
	models, err := u.store.Models(operation.Action, plan.IDs)
	if err != nil {
		return nil, err
	}
	preview, err := plan.Complete(models, u.store.VendorConflict)
	if err != nil {
		return nil, err
	}
	return preview, nil
}
