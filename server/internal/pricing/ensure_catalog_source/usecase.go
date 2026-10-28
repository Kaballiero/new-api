package ensure_catalog_source

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Store interface {
	EnsureSource(context.Context, domain.CatalogSource) (domain.CatalogSource, error)
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute(ctx context.Context, source domain.CatalogSource) (domain.CatalogSource, error) {
	if !domain.ValidCatalogHash(source.ID) || source.Kind == "" || len(source.Kind) > 32 || source.BaseURL == "" {
		return domain.CatalogSource{}, fmt.Errorf("invalid catalog source")
	}
	current, err := u.store.EnsureSource(ctx, source)
	if err != nil {
		return domain.CatalogSource{}, err
	}
	if current.Kind != source.Kind || current.BaseURL != source.BaseURL {
		return domain.CatalogSource{}, fmt.Errorf("catalog source identity mismatch")
	}
	return current, nil
}
