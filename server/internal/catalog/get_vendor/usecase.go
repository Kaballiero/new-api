package get_vendor

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	Vendor(int) (*domain.Vendor, error)
	IsMissing(error) bool
}
type Usecase struct {
	store   Store
	version func(*domain.Vendor) string
}

func NewUsecase(store Store, version func(*domain.Vendor) string) Usecase {
	return Usecase{store: store, version: version}
}
func (u Usecase) Execute(input Input) (*Output, error) {
	value, err := u.store.Vendor(input.ID)
	if err != nil {
		if u.store.IsMissing(err) {
			return nil, domain.ErrVendorNotFound
		}
		return nil, domain.Internal(err)
	}
	value.Version = u.version(value)
	return value, nil
}
