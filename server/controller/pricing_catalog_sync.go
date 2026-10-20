package controller

import (
	"context"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/model"
)

func refreshUpstreamCatalog(ctx context.Context, source model.CatalogSource) (pricingCatalogRefreshSummary, error) {
	return app.NewRefreshPricingCatalogue().RefreshUpstream(ctx, domain.CatalogSource(source))
}
