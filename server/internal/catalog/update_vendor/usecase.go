package update_vendor

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	VendorDuplicate(int, string) (bool, error)
	WithVendorMetadata(func(domain.VendorMetadataMutation) error) error
	IsMissing(error) bool
}
type Usecase struct {
	now     func() int64
	refresh func()

	store   Store
	version func(*domain.Vendor) string
}

func NewUsecase(store Store, now func() int64, refresh func(), version func(*domain.Vendor) string) Usecase {
	return Usecase{store: store, now: now, refresh: refresh, version: version}
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
	return &v, nil
}

func (u Usecase) persist(value *domain.Vendor) error {
	err := u.store.WithVendorMetadata(func(tx domain.VendorMetadataMutation) error {
		saved, err := tx.SavedVendor(value.Id)
		if err != nil {
			return err
		}
		if value.Version != "" && value.Version != u.version(saved) {
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
		value.Version = u.version(value)
		u.refresh()
	}
	return err
}
