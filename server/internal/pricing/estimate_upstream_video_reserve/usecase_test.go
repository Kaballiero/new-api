package estimate_upstream_video_reserve

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type publishedTariffs struct {
	tariffs *domain.UpstreamCatalogTariffs
}

func (published publishedTariffs) Load() *domain.UpstreamCatalogTariffs { return published.tariffs }

// liveVideoStub discovers one video tariff or fails, counting its calls.
type liveVideoStub struct {
	tariff domain.VideoModelPricing
	err    error
	calls  int
}

func (live *liveVideoStub) Discover(model string) (domain.VideoModelPricing, error) {
	live.calls++
	if live.err != nil {
		return domain.VideoModelPricing{}, live.err
	}
	return live.tariff, nil
}

var errUnpriced = errors.New("OpenRouter reservation unavailable: no compatible output tariff")

// orderStub prices a tariff by its per-second SKU; any other SKU value fails.
type orderStub map[string]float64

func (order orderStub) Price(tariff domain.VideoModelPricing) (float64, error) {
	usd, ok := order[tariff.PricingSKUs["cents_per_second_output"]]
	if !ok {
		return 0, errUnpriced
	}
	return usd, nil
}

// A video reserve is priced from the fresh catalog without live; live prices
// it otherwise and checks a catalog tariff the order cannot price; a stale
// document prices it only when live fails.
func TestExecuteSourceOrder(t *testing.T) {
	const model = "runway/gen-4.5"
	const official = "https://openrouter.ai/api"
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	tariff := func(sku string) domain.VideoModelPricing {
		return domain.VideoModelPricing{ID: model, PricingSKUs: map[string]string{"cents_per_second_output": sku}}
	}
	catalogAt := func(fetchedAt time.Time, sku string) *domain.UpstreamCatalogTariffs {
		return &domain.UpstreamCatalogTariffs{FetchedAt: fetchedAt, Videos: map[string]domain.VideoModelPricing{model: tariff(sku)}}
	}
	fresh, stale := now.Add(-time.Hour), now.Add(-25*time.Hour)
	liveDown := errors.New("OpenRouter reservation unavailable: video discovery failed")
	order := orderStub{"12": 1.2, "20": 2}
	for _, tc := range []struct {
		name      string
		published *domain.UpstreamCatalogTariffs
		baseURL   string
		liveErr   error
		want      Estimate
		wantErr   error
		liveCalls int
	}{
		{name: "fresh catalog prices without live", published: catalogAt(fresh, "12"), baseURL: official, want: Estimate{USD: 1.2, Source: domain.ReserveSourceCatalog}},
		{name: "catalog price error is priced again from live", published: catalogAt(fresh, "99"), baseURL: official, want: Estimate{USD: 2, Source: domain.ReserveSourceLive}, liveCalls: 1},
		{name: "catalog price error stands when the live check fails", published: catalogAt(fresh, "99"), baseURL: official, liveErr: liveDown, wantErr: errUnpriced, liveCalls: 1},
		{name: "stale catalog goes live", published: catalogAt(stale, "12"), baseURL: official, want: Estimate{USD: 2, Source: domain.ReserveSourceLive}, liveCalls: 1},
		{name: "stale catalog prices when live fails", published: catalogAt(stale, "12"), baseURL: official, liveErr: liveDown, want: Estimate{USD: 1.2, Source: domain.ReserveSourceCatalogStale}, liveCalls: 1},
		{name: "unofficial base URL goes live", published: catalogAt(fresh, "12"), baseURL: "https://proxy.example/api", want: Estimate{USD: 2, Source: domain.ReserveSourceLive}, liveCalls: 1},
		{name: "unofficial base URL never takes a stale document", published: catalogAt(stale, "12"), baseURL: "https://proxy.example/api", liveErr: liveDown, wantErr: liveDown, liveCalls: 1},
		{name: "model absent from the catalog goes live", published: &domain.UpstreamCatalogTariffs{FetchedAt: fresh}, baseURL: official, want: Estimate{USD: 2, Source: domain.ReserveSourceLive}, liveCalls: 1},
		{name: "no published catalog goes live", baseURL: official, want: Estimate{USD: 2, Source: domain.ReserveSourceLive}, liveCalls: 1},
		{name: "live error without a document", baseURL: official, liveErr: liveDown, wantErr: liveDown, liveCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := &liveVideoStub{tariff: tariff("20"), err: tc.liveErr}
			usecase := NewUsecase(publishedTariffs{tc.published}, func() time.Time { return now }, func() time.Duration { return 24 * time.Hour })
			estimate, err := usecase.Execute(Input{Model: model, ChannelBaseURL: tc.baseURL, Live: live, Order: order})
			assert.Equal(t, tc.liveCalls, live.calls)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, estimate)
		})
	}
}
