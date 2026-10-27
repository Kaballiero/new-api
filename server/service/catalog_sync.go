package service

import (
	"context"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/sync_upstream_catalog"
	"github.com/QuantumNous/new-api/model"
	"time"
)

type CatalogSyncResult = sync_upstream_catalog.CatalogSyncResult

func SyncUpstreamCatalog(ctx context.Context, source model.CatalogSource, requested map[string]bool, deps domain.Capabilities, client sync_upstream_catalog.Client) (CatalogSyncResult, error) {
	return sync_upstream_catalog.NewUsecase(deps, model.NewImmutableCatalogPersistence(), time.Now, model.InvalidatePricingCache).Execute(ctx, domain.CatalogSource(source), requested, client)
}
