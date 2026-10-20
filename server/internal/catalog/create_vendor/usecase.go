package create_vendor

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
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return nil, &domain.Failure{Kind: "vendor_name_empty", Message: "供应商名称不能为空"}
	}
	if dup, err := u.store.VendorDuplicate(0, v.Name); err != nil {
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
	value.Id = 0
	err := u.store.WithVendorMetadata(func(tx domain.VendorMetadataMutation) error {
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
		now := u.now()
		value.CreatedTime, value.UpdatedTime, value.Status = now, now, 1
		return tx.InsertVendor(value)
	})
	if err == nil {
		value.Version = domain.VendorRecordVersion(value)
		u.refresh()
	}
	return err
}
