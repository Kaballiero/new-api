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
	audit   func(Input, Output)
}

func NewUsecase(store Store, now func() int64, refresh func(), audit func(Input, Output)) Usecase {
	return Usecase{store: store, now: now, refresh: refresh, audit: audit}
}
func (u Usecase) Execute(input Input) (*Output, error) {
	operation := input
	if operation.ExpectedVersion == "" {
		return nil, domain.RejectVendorChange(domain.ErrVendorConflict, false)
	}
	result := &domain.VendorOperationResult{UpdatedModels: []int{}, DeletedVendors: []int{}}
	err := u.store.WithVendorMutation(func(tx domain.VendorMutation) error {
		plan, err := domain.NewVendorOperationPlan(operation)
		if err != nil {
			return err
		}
		vendors, err := tx.Vendors()
		if err != nil {
			return err
		}
		if err := plan.SetVendors(vendors); err != nil {
			return err
		}
		models, err := tx.Models(operation.Action, plan.IDs)
		if err != nil {
			return err
		}
		preview, err := plan.Complete(models)
		if err != nil {
			return err
		}

		if operation.ExpectedVersion != "" && operation.ExpectedVersion != preview.Version {
			return domain.ErrVendorConflict
		}
		for _, item := range preview.Models {
			if item.VendorID != operation.TargetVendorID {
				result.UpdatedModels = append(result.UpdatedModels, item.ID)
			}
		}
		if len(result.UpdatedModels) > 0 {
			if err := tx.Assign(result.UpdatedModels, operation.TargetVendorID, u.now()); err != nil {
				return err
			}
		}
		if operation.Action == "merge" || operation.Action == "delete" {
			for _, vendor := range preview.Sources {
				result.DeletedVendors = append(result.DeletedVendors, vendor.Id)
			}
			if err := tx.Delete(result.DeletedVendors); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
	}
	u.refresh()
	u.audit(input, *result)
	return result, nil
}
