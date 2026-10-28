package domain

func ApplyVendorOperation(tx VendorMutation, operation VendorOperation, result *VendorOperationResult, now func() int64, versions OperationVersions) error {
	plan, err := NewVendorOperationPlan(operation)
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
	preview, err := plan.Complete(models, versions)
	if err != nil {
		return err
	}

	if operation.ExpectedVersion != "" && operation.ExpectedVersion != preview.Version {
		return ErrVendorConflict
	}
	for _, item := range preview.Models {
		if item.VendorID != operation.TargetVendorID {
			result.UpdatedModels = append(result.UpdatedModels, item.ID)
		}
	}
	if len(result.UpdatedModels) > 0 {
		if err := tx.Assign(result.UpdatedModels, operation.TargetVendorID, now()); err != nil {
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
}
