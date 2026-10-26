package search_vendors

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	Vendors(string, int, int, string) ([]*domain.Vendor, int64, error)
}
type Usecase struct {
	store   Store
	version func(*domain.Vendor) string
}

func NewUsecase(store Store, version func(*domain.Vendor) string) Usecase {
	return Usecase{store: store, version: version}
}
func (u Usecase) Execute(input Input) (Output, error) {
	v, n, e := u.store.Vendors(input.Keyword, (input.Page-1)*input.PageSize, input.PageSize, input.Association)
	if e != nil {
		return Output{}, domain.Internal(e)
	}
	for _, vendor := range v {
		vendor.Version = u.version(vendor)
	}
	return Output{Page: input.Page, PageSize: input.PageSize, Total: int(n), Items: v}, nil
}
