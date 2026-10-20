package update_vendor

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"strings"
)

type Store interface {
	VendorDuplicate(int, string) (bool, error)
	WithVendorMetadata(func(domain.VendorMetadataMutation) error) error
	IsMissing(error) bool
	VendorConflict() error
}
type Usecase struct {
	now     func() int64
	refresh func()

	store Store
	audit func(int, string)
}

func NewUsecase(store Store, now func() int64, refresh func(), audit func(int, string)) Usecase {
	return Usecase{store: store, now: now, refresh: refresh, audit: audit}
}
func (u Usecase) Execute(input Input) (*Output, *domain.Failure) {
	v := input.Value
	if v.Id == 0 {
		return nil, &domain.Failure{Kind: "vendor_id_missing", Message: "缺少供应商 ID"}
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return nil, &domain.Failure{Kind: "vendor_name_empty", Message: "供应商名称不能为空"}
	}
	if dup, err := u.store.VendorDuplicate(v.Id, v.Name); err != nil {
		return nil, &domain.Failure{Kind: "internal_error", Cause: err}
	} else if dup {
		return nil, &domain.Failure{Kind: "vendor_name_exists", Message: "供应商名称已存在"}
	}
	if err := u.persist(&v); err != nil {
		if u.store.IsMissing(err) {
			return nil, &domain.Failure{Kind: "vendor_not_found", Message: "vendor not found"}
		}
		if errors.Is(err, u.store.VendorConflict()) {
			return nil, &domain.Failure{Kind: "vendor_conflict", Cause: err}
		}
		return nil, &domain.Failure{Kind: "vendor", Cause: err}
	}
	u.audit(v.Id, v.Name)
	return &v, nil
}

func (u Usecase) persist(value *domain.Vendor) error {
	err := u.store.WithVendorMetadata(func(tx domain.VendorMetadataMutation) error {
		saved, err := tx.SavedVendor(value.Id)
		if err != nil {
			return err
		}
		if value.Version != "" && value.Version != domain.VendorRecordVersion(saved) {
			return u.store.VendorConflict()
		}
		if err := domain.NormalizeVendor(value); err != nil {
			return err
		}
		others, err := tx.OtherVendors(value.Id)
		if err != nil {
			return err
		}
		if err := domain.ValidateVendorName(value, others); err != nil {
			return err
		}
		value.CreatedTime, value.Status, value.UpdatedTime = saved.CreatedTime, saved.Status, u.now()
		return tx.UpdateVendor(value)
	})
	if err == nil {
		value.Version = domain.VendorRecordVersion(value)
		u.refresh()
	}
	return err
}
