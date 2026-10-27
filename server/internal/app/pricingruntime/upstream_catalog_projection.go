package pricingruntime

import (
	"context"
	"sync"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/read_upstream_catalogue"
)

func LoadConfiguredCatalog(ctx context.Context) (map[string]*pricingdomain.SourceDocument, map[string]string, bool, error) {
	return cachedReadUpstreamCatalogue().Execute(ctx)
}

var cachedReadUpstreamCatalogue = sync.OnceValue(func() read_upstream_catalogue.Usecase {
	return read_upstream_catalogue.NewUsecase(
		pricingadapter.Capabilities(),
		pricingstate.NewImmutableCatalogPersistence(),
		func() (pricingdomain.CatalogSource, bool, error) {
			v, on, err := pricingstate.ConfiguredCatalogSource()
			return pricingdomain.CatalogSource(v), on, err
		},
	)
})
