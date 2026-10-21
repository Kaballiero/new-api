package get_vendor

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	Vendor(int) (*domain.Vendor, error)
	IsMissing(error) bool
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute(input Input) (*Output, error) {
	value, err := u.store.Vendor(input.ID)
	if err != nil {
		if u.store.IsMissing(err) {
			return nil, domain.ErrVendorNotFound
		}
		return nil, domain.Internal(err)
	}
	value.Version = domain.VendorRecordVersion(value)
	return value, nil
}
