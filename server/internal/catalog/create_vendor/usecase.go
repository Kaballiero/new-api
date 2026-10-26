package create_vendor

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
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return nil, domain.ErrVendorNameEmpty
	}
	if dup, err := u.store.VendorDuplicate(0, v.Name); err != nil {
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
		value.Version = u.version(value)
		u.refresh()
	}
	return err
}
