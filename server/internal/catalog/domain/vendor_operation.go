package domain

import (
	"errors"
	"fmt"
	"slices"
)

type VendorOperationPlan struct {
	Operation VendorOperation
	IDs       []int
	Preview   *VendorOperationPreview
	Vendors   map[int]*Vendor
}

func NewVendorOperationPlan(operation VendorOperation) (*VendorOperationPlan, error) {
	if operation.Action != "assign" && operation.Action != "merge" && operation.Action != "delete" {
		return nil, errors.New("unsupported vendor operation")
	}
	ids := append([]int{}, operation.VendorIDs...)
	if operation.Action == "assign" {
		ids = append([]int{}, operation.ModelIDs...)
	}
	if len(ids) == 0 || len(ids) > 1000 {
		return nil, errors.New("select between 1 and 1000 records")
	}
	slices.Sort(ids)
	for i, id := range ids {
		if id <= 0 || i > 0 && ids[i-1] == id {
			return nil, errors.New("invalid or duplicate selection")
		}
	}
	preview := &VendorOperationPreview{Action: operation.Action, Sources: []Vendor{}, Models: []VendorAssignmentModel{}}
	return &VendorOperationPlan{Operation: operation, IDs: ids, Preview: preview}, nil
}
func (plan *VendorOperationPlan) SetVendors(vendors []Vendor) error {
	operation, ids, preview := plan.Operation, plan.IDs, plan.Preview
	byID := make(map[int]*Vendor, len(vendors))
	for i := range vendors {
		byID[vendors[i].Id] = &vendors[i]
	}
	if operation.Action != "delete" {
		if operation.TargetVendorID < 0 || operation.Action == "merge" && operation.TargetVendorID == 0 {
			return errors.New("select a saved target vendor")
		}
		if operation.TargetVendorID != 0 {
			preview.Target = byID[operation.TargetVendorID]
			if preview.Target == nil {
				return fmt.Errorf("%w: target vendor does not exist", ErrVendorConflict)
			}
		}
	}
	if operation.Action == "assign" {

	} else {
		for _, id := range ids {
			vendor := byID[id]
			if vendor == nil {
				return fmt.Errorf("%w: source vendor does not exist", ErrVendorConflict)
			}
			if operation.Action == "merge" && id == operation.TargetVendorID {
				return errors.New("target vendor cannot also be a source")
			}
			preview.Sources = append(preview.Sources, *vendor)
		}
	}
	plan.Vendors = byID
	return nil
}
func (plan *VendorOperationPlan) Complete(models []Model, versions OperationVersions) (*VendorOperationPreview, error) {
	operation, ids, preview, byID := plan.Operation, plan.IDs, plan.Preview, plan.Vendors
	if operation.Action == "assign" && len(models) != len(ids) {
		return nil, fmt.Errorf("%w: a selected model no longer exists", ErrVendorConflict)
	}
	seen := make(map[int]bool)
	modelVersions := make([]string, 0, len(models))
	counts := make(map[int]int64)
	for _, item := range models {
		modelVersions = append(modelVersions, versions.MetadataRecordVersion(&item, nil, nil))
		row := VendorAssignmentModel{ID: item.Id, ModelName: item.ModelName, NameRule: item.NameRule, VendorID: item.VendorID, UpdatedTime: item.UpdatedTime}
		if vendor := byID[item.VendorID]; vendor != nil {
			row.VendorName = vendor.Name
			if operation.Action == "assign" && !seen[vendor.Id] {
				preview.Sources = append(preview.Sources, *vendor)
				seen[vendor.Id] = true
			}
		}
		preview.Models = append(preview.Models, row)
		counts[item.VendorID]++
	}
	if operation.Action == "delete" && len(models) > 0 {
		return nil, &VendorReferenceError{Counts: counts}
	}
	slices.SortFunc(preview.Sources, func(a, b Vendor) int { return a.Id - b.Id })
	version, err := versions.VendorOperationVersion(preview, modelVersions)
	if err != nil {
		return nil, err
	}
	preview.Version = version
	return preview, nil
}

type VendorMutation interface {
	Vendors() ([]Vendor, error)
	Models(string, []int) ([]Model, error)
	Assign([]int, int, int64) error
	Delete([]int) error
}
