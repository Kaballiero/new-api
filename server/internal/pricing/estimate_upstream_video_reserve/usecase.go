package estimate_upstream_video_reserve

import (
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

// CatalogTariffs reads the tariffs of the published upstream catalog
// revision; nil before any.
type CatalogTariffs interface {
	Load() *domain.UpstreamCatalogTariffs
}

// LiveVideoTariffs discovers the video tariffs OpenRouter publishes for a
// model at request time; the gateway implements it on the channel of the
// request.
type LiveVideoTariffs interface {
	Discover(model string) (domain.VideoModelPricing, error)
}

// VideoOrder prices a prepared video order from the tariffs of its model; the
// gateway implements it with its SKU rules.
type VideoOrder interface {
	Price(domain.VideoModelPricing) (float64, error)
}

// Input is a video order the gateway has prepared.
type Input struct {
	Model string
	// ChannelBaseURL is the base URL of the channel; only the official
	// OpenRouter API is priced from the upstream catalog.
	ChannelBaseURL string
	Live           LiveVideoTariffs
	Order          VideoOrder
}

// Estimate is the reserve of a video order in USD with what it is priced
// from: ReserveSourceCatalog, ReserveSourceLive or ReserveSourceCatalogStale.
type Estimate struct {
	USD    float64
	Source domain.ReserveSource
}

type Usecase struct {
	catalog CatalogTariffs
	now     func() time.Time
	// maxAge bounds the age of the catalog data a reserve is priced from.
	maxAge func() time.Duration
}

func NewUsecase(catalog CatalogTariffs, now func() time.Time, maxAge func() time.Duration) Usecase {
	return Usecase{catalog: catalog, now: now, maxAge: maxAge}
}

// Execute prices the order from the video tariff of its model: the catalog
// tariff while the catalog data is at most maxAge old and the channel is on
// the official OpenRouter API, otherwise the live tariff, and when live fails
// a stale catalog tariff (domain.ModelTariff). A fresh catalog tariff the
// order cannot price is checked against live once: the live tariff prices the
// order then, and when live fails the catalog pricing error stands.
func (u Usecase) Execute(in Input) (Estimate, error) {
	published := u.catalog.Load()
	var catalog map[string]domain.VideoModelPricing
	fresh := false
	if published != nil && domain.IsOpenRouterCatalogBaseURL(in.ChannelBaseURL) {
		catalog = published.Videos
		fresh = published.FreshAt(u.now(), u.maxAge())
	}
	tariff, source, err := domain.ModelTariff(in.Model, catalog, fresh, in.Live)
	if source == "" {
		return Estimate{}, err
	}
	usd, err := in.Order.Price(tariff)
	if err != nil && source == domain.ReserveSourceCatalog {
		live, liveErr := in.Live.Discover(in.Model)
		if liveErr == nil {
			usd, err = in.Order.Price(live)
			source = domain.ReserveSourceLive
		}
	}
	if err != nil {
		return Estimate{}, err
	}
	return Estimate{USD: usd, Source: source}, nil
}
