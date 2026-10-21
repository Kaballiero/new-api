package update_vendor

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"strings"
)

type Store interface {
	VendorDuplicate(int, string) (bool, error)
	WithVendorMetadata(func(domain.VendorMetadataMutation) error) error
	IsMissing(error) bool
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
func (u Usecase) Execute(input Input) (*Output, error) {
	v := input.Value
	if v.Id == 0 {
		return nil, domain.ErrVendorIDMissing
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return nil, domain.ErrVendorNameEmpty
	}
	if dup, err := u.store.VendorDuplicate(v.Id, v.Name); err != nil {
		return nil, domain.Internal(err)
	} else if dup {
		return nil, domain.ErrVendorNameExists
	}
	if err := u.persist(&v); err != nil {
		return nil, domain.RejectVendorChange(err, u.store.IsMissing(err))
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
			return domain.ErrVendorConflict
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
