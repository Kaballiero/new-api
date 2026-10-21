package update_vendor_record

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	WithVendorMetadata(func(domain.VendorMetadataMutation) error) error
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
