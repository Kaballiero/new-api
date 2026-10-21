package search_vendors

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	Vendors(string, int, int, string) ([]*domain.Vendor, int64, error)
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute(input Input) (Output, error) {
	v, n, e := u.store.Vendors(input.Keyword, (input.Page-1)*input.PageSize, input.PageSize, input.Association)
	if e != nil {
		return Output{}, domain.Internal(e)
	}
	for _, vendor := range v {
		vendor.Version = domain.VendorRecordVersion(vendor)
	}
	return Output{Page: input.Page, PageSize: input.PageSize, Total: int(n), Items: v}, nil
}
