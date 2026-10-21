package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	"github.com/QuantumNous/new-api/internal/catalog/apply_vendor_operation"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/preview_vendor_operation"
	"github.com/QuantumNous/new-api/internal/catalog/remove_vendors"
)

var ErrVendorConflict = domain.ErrVendorConflict

// VendorReferenceError reports every referenced vendor in a rejected delete.
type VendorReferenceError = domain.VendorReferenceError

func FindMetadataVendor(vendors map[string]*Vendor, name string) *Vendor {
	for _, vendor := range vendors {
		if domain.VendorNameEqual(vendor.Name, name) {
			return vendor
		}
	}
	return nil
}

func VendorRecordVersion(v *Vendor) string {
	return domain.VendorRecordVersion(record.ToVendor((*record.Vendor)(v)))
}

// VendorOperation uses explicit selections for both preview and application.
type VendorOperation = domain.VendorOperation

type VendorAssignmentModel = domain.VendorAssignmentModel

type VendorOperationPreview = domain.VendorOperationPreview

type VendorOperationResult = domain.VendorOperationResult

func PreviewVendorOperation(operation VendorOperation) (*VendorOperationPreview, error) {
	return preview_vendor_operation.NewUsecase(catalogVendorOperationBridge{}).Execute(operation)
}

func ApplyVendorOperation(operation VendorOperation) (*VendorOperationResult, error) {
	return apply_vendor_operation.NewUsecase(catalogVendorOperationBridge{}, common.GetTimestamp, RefreshPricing, func(apply_vendor_operation.Input, apply_vendor_operation.Output) {}).Execute(operation)
}

func DeleteVendors(ids []int) error {
	_, err := remove_vendors.NewUsecase(catalogVendorOperationBridge{}, common.GetTimestamp, RefreshPricing).Execute(ids)
	return err
}
