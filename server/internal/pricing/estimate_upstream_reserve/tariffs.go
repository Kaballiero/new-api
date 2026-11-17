package estimate_upstream_reserve

import (
	"slices"
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

// LiveTariffs discovers the endpoint tariffs OpenRouter publishes for an
// upstream model at request time; the gateway implements it on the channel of
// the request.
type LiveTariffs interface {
	Discover(model string) (domain.NativeEndpointResponse, error)
}

// CatalogTariffs reads the endpoint tariffs of the published upstream catalog
// revision; nil before any.
type CatalogTariffs interface {
	Load() *domain.UpstreamCatalogTariffs
}

// modelTariffs are the endpoint tariffs a routable model is priced from.
type modelTariffs struct {
	response domain.NativeEndpointResponse
	// source is ReserveSourceCatalog, ReserveSourceCatalogStale or
	// ReserveSourceLive.
	source domain.ReserveSource
}

// requestTariffs are the tariffs of the routable models of a request by tariff
// model (domain.UpstreamTariffModel).
type requestTariffs map[string]modelTariffs

// fromCatalog tells whether a routable model is priced from a catalog
// document.
func (tariffs requestTariffs) fromCatalog() bool {
	for _, tariff := range tariffs {
		if tariff.source != domain.ReserveSourceLive {
			return true
		}
	}
	return false
}

// tariffSourcePrecedence orders the sources of the model tariffs of a request:
// the first one any model is priced from names the reserve.
var tariffSourcePrecedence = []domain.ReserveSource{domain.ReserveSourceCatalogStale, domain.ReserveSourceLive, domain.ReserveSourceCatalog}

// loadTariffs takes the tariffs of every routable model once per tariff model.
// A channel on the official OpenRouter API takes the catalog document of the
// model while the catalog data is at most maxAgeHours old: its fetchedAt is
// the time the data last changed, so a catalog nobody refreshed goes stale and
// the reserve falls back to live. Otherwise the model is discovered live; when
// live fails, a stale document of the model is taken, and without one the live
// error is returned.
func (u Usecase) loadTariffs(in Input) (requestTariffs, error) {
	published := u.catalog.Load()
	var catalog map[string]domain.NativeEndpointResponse
	fresh := false
	if published != nil && domain.IsOpenRouterCatalogBaseURL(in.ChannelBaseURL) {
		catalog = published.Models
		fresh = !u.now().After(published.FetchedAt.Add(time.Duration(u.maxAgeHours()) * time.Hour))
	}
	tariffs := make(requestTariffs, len(in.Models))
	for _, model := range in.Models {
		tariffModel := domain.UpstreamTariffModel(model, in.Batch)
		if _, loaded := tariffs[tariffModel]; loaded {
			continue
		}
		document, documented := catalog[tariffModel]
		if documented && fresh {
			tariffs[tariffModel] = modelTariffs{response: document, source: domain.ReserveSourceCatalog}
			continue
		}
		response, err := in.Live.Discover(tariffModel)
		if err == nil {
			tariffs[tariffModel] = modelTariffs{response: response, source: domain.ReserveSourceLive}
			continue
		}
		if !documented {
			return nil, err
		}
		tariffs[tariffModel] = modelTariffs{response: document, source: domain.ReserveSourceCatalogStale}
	}
	return tariffs, nil
}

// recheckLive replaces the fresh catalog tariffs with live ones. It fails when
// live fails or a model has a stale document: live already failed for it.
func (tariffs requestTariffs) recheckLive(live LiveTariffs) (requestTariffs, bool) {
	for _, tariff := range tariffs {
		if tariff.source == domain.ReserveSourceCatalogStale {
			return nil, false
		}
	}
	rechecked := make(requestTariffs, len(tariffs))
	for model, tariff := range tariffs {
		if tariff.source == domain.ReserveSourceCatalog {
			response, err := live.Discover(model)
			if err != nil {
				return nil, false
			}
			tariff = modelTariffs{response: response, source: domain.ReserveSourceLive}
		}
		rechecked[model] = tariff
	}
	return rechecked, true
}

// reserveSource names what the reserve of a request is priced from: the
// configured price, then the approximate (manual) reserve of the channel,
// then the model tariffs by tariffSourcePrecedence.
func reserveSource(in Input, tariffs requestTariffs) domain.ReserveSource {
	if in.ConfiguredPrice {
		return domain.ReserveSourceConfiguredPrice
	}
	if in.ApproximateReserveUSD != nil {
		return domain.ReserveSourceManual
	}
	source := domain.ReserveSourceCatalog
	for _, tariff := range tariffs {
		if slices.Index(tariffSourcePrecedence, tariff.source) < slices.Index(tariffSourcePrecedence, source) {
			source = tariff.source
		}
	}
	return source
}
