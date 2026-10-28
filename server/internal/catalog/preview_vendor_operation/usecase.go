package preview_vendor_operation

import "github.com/QuantumNous/new-api/internal/catalog/domain"

type Store interface {
	IsMissing(error) bool
	Vendors() ([]domain.Vendor, error)
	Models(string, []int) ([]domain.Model, error)
}
type Usecase struct {
	store    Store
	versions domain.OperationVersions
}

func NewUsecase(store Store, versions domain.OperationVersions) Usecase {
	return Usecase{store: store, versions: versions}
}
func (u Usecase) Execute(input Input) (*Output, error) {
	operation := input
	plan, err := domain.NewVendorOperationPlan(operation)
	if err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
	}
	vendors, err := u.store.Vendors()
	if err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
	}
	if err := plan.SetVendors(vendors); err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
	}
	models, err := u.store.Models(operation.Action, plan.IDs)
	if err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
	}
	preview, err := plan.Complete(models, u.versions)
	if err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
	}
	return preview, nil
}
