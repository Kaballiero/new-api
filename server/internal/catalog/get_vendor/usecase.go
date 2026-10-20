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
func (u Usecase) Execute(input Input) (*Output, *domain.Failure) {
	value, err := u.store.Vendor(input.ID)
	if err != nil {
		if u.store.IsMissing(err) {
			return nil, &domain.Failure{Kind: "vendor_not_found", Message: "vendor not found"}
		}
		return nil, &domain.Failure{Kind: "internal_error", Cause: err}
	}
	value.Version = domain.VendorRecordVersion(value)
	return value, nil
}
