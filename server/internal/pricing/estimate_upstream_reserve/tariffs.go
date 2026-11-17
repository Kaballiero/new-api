package estimate_upstream_reserve

import (
	"errors"
	"slices"

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
	// liveErr is the failure of the live check of a catalog tariff.
	liveErr error
}

// checksLive tells whether the tariff is a fresh catalog document not checked
// against live yet.
func (tariff modelTariffs) checksLive() bool {
	return tariff.source == domain.ReserveSourceCatalog && tariff.liveErr == nil
}

// missesCatalogPrice tells whether err is a price missing from a tariff that
// checks live.
func (tariff modelTariffs) missesCatalogPrice(err error) bool {
	return tariff.checksLive() && errors.Is(err, domain.ErrEndpointPriceMissing)
}

// requestTariffs are the tariffs of the routable models of a request by tariff
// model (domain.UpstreamTariffModel).
type requestTariffs map[string]modelTariffs

// recheckLive replaces the catalog tariff of a model with its live tariff.
// When live fails, the document stays with the live error (price decides what
// it may still price).
func (tariffs requestTariffs) recheckLive(live LiveTariffs, model string) {
	response, err := live.Discover(model)
	if err != nil {
		tariff := tariffs[model]
		tariff.liveErr = err
		tariffs[model] = tariff
		return
	}
	tariffs[model] = modelTariffs{response: response, source: domain.ReserveSourceLive}
}

// hasStaleDocument tells whether a stale catalog document prices a model.
func (tariffs requestTariffs) hasStaleDocument() bool {
	for _, tariff := range tariffs {
		if tariff.source == domain.ReserveSourceCatalogStale {
			return true
		}
	}
	return false
}

// tariffSourcePrecedence orders the sources of the model tariffs of a request:
// the first one any model is priced from names the reserve.
var tariffSourcePrecedence = []domain.ReserveSource{domain.ReserveSourceCatalogStale, domain.ReserveSourceLive, domain.ReserveSourceCatalog}

// loadTariffs takes the tariffs of every routable model once per tariff model.
// The catalog prices text generation and embeddings, Batch included, on a
// channel on the official OpenRouter API; rerank, decisions, audio and image
// output stay live. Such a model takes the catalog document of its tariff
// model while the catalog data is at most maxAge old: its fetchedAt is the
// time the data last changed, so a catalog nobody refreshed goes stale and the
// reserve falls back to live. Otherwise the model is discovered live; when
// live fails, a stale document of the model is taken, and without one the live
// error is returned.
func (u Usecase) loadTariffs(in Input) (requestTariffs, error) {
	published := u.catalog.Load()
	liveOperation := in.Rerank || in.Decisions || in.Audio || in.ImageOutput || in.ImageReserveOnly
	var catalog map[string]domain.NativeEndpointResponse
	fresh := false
	if published != nil && !liveOperation && domain.IsOpenRouterCatalogBaseURL(in.ChannelBaseURL) {
		catalog = published.Models
		fresh = !u.now().After(published.FetchedAt.Add(u.maxAge()))
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
