package controller

import (
	"context"

	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"

	upstream "github.com/QuantumNous/new-api/internal/adapter/pricing/upstream"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/refresh_catalogue"
	"github.com/QuantumNous/new-api/model"
)

const pricingCatalogWorkers = 5
const pricingCatalogBodyLimit = 2 << 20
const pricingCatalogFailureLimit = 20

func refreshChannelPricingCache() {
	channelstore.InitChannelCache()
	enqueuePricingCatalogRefresh()
}
func enqueuePricingCatalogRefresh() { app.NewEnqueuePricingCatalogueRefresh().Execute() }

type pricingCatalogSource struct {
	channel     *model.Channel
	upstream    string
	endpoint    constant.EndpointType
	fingerprint string
}

type pricingCatalogRefreshSummary = domain.CatalogRefreshSummary
type pricingCatalogFailure = domain.CatalogRefreshFailure

func pricingNativeSource(v pricingCatalogSource) refresh_catalogue.Source {
	var channel refresh_catalogue.Channel
	if v.channel != nil {
		channel = upstream.NewNativeChannel(v.channel)
	}
	return refresh_catalogue.Source{Channel: channel, Upstream: v.upstream, Endpoint: v.endpoint, Fingerprint: v.fingerprint}
}
func pricingLegacySource(v refresh_catalogue.Source) pricingCatalogSource {
	var channel *model.Channel
	if v.Channel != nil {
		channel = v.Channel.(upstream.NativeChannel).Native()
	}
	return pricingCatalogSource{channel, v.Upstream, v.Endpoint, v.Fingerprint}
}
func pricingCatalogSources(ctx context.Context) ([]pricingCatalogSource, error) {
	rows, err := app.NewRefreshPricingCatalogue().DiscoverSources(ctx)
	if rows == nil {
		return nil, err
	}
	out := make([]pricingCatalogSource, len(rows))
	for i, v := range rows {
		out[i] = pricingLegacySource(v)
	}
	return out, err
}
func refreshPricingCatalogSources(ctx context.Context, sources []pricingCatalogSource, fetch func(context.Context, pricingCatalogSource) (domain.PricingCatalogEntry, error), publish func([]domain.PricingCatalogEntry) (int, error)) (pricingCatalogRefreshSummary, error) {
	var rows []refresh_catalogue.Source
	for _, v := range sources {
		rows = append(rows, pricingNativeSource(v))
	}
	return app.NewRefreshPricingCatalogue().RefreshSources(ctx, rows, func(ctx context.Context, v refresh_catalogue.Source) (domain.PricingCatalogEntry, error) {
		return fetch(ctx, pricingLegacySource(v))
	}, publish)
}
func fetchPricingCatalogSource(ctx context.Context, source pricingCatalogSource) (domain.PricingCatalogEntry, error) {
	return app.NewRefreshPricingCatalogue().FetchSource(ctx, pricingNativeSource(source))
}
func selectPricingCatalogOffers(offers []domain.CatalogOffer) []domain.CatalogOffer {
	return domain.SelectPricingCatalogOffers(offers)
}
func normalizeEndpointPricing(upstream, operation, main string, body []byte) ([]domain.CatalogOffer, error) {
	return app.NewPricingNativeNormalizer().NormalizeEndpointPricing(upstream, operation, main, body)
}
func normalizeImagePricing(upstream string, body []byte) ([]domain.CatalogOffer, error) {
	return app.NewPricingNativeNormalizer().NormalizeImagePricing(upstream, body)
}
