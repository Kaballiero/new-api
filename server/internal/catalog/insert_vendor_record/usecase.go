package insert_vendor_record

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	WithVendorMetadata(func(domain.VendorMetadataMutation) error) error
	VendorConflict() error
}
type Usecase struct {
	store   Store
	now     func() int64
	refresh func()
}

func NewUsecase(store Store, now func() int64, refresh func()) Usecase {
	return Usecase{store: store, now: now, refresh: refresh}
}
func (u Usecase) Execute(value *domain.Vendor) error {
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
