package get_catalog_freshness_test

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/get_catalog_freshness"

	"github.com/stretchr/testify/assert"
)

type publishedTariffs struct {
	tariffs *domain.UpstreamCatalogTariffs
}

func (p publishedTariffs) Load() *domain.UpstreamCatalogTariffs { return p.tariffs }

func TestCatalogFreshness(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	fetchedAt := now.Add(-24 * time.Hour)
	cases := []struct {
		name    string
		tariffs *domain.UpstreamCatalogTariffs
		maxAge  time.Duration
		want    get_catalog_freshness.Output
	}{
		{name: "no published catalog", maxAge: 24 * time.Hour, want: get_catalog_freshness.Output{MaxAgeHours: 24}},
		{name: "exactly max age old is fresh", tariffs: &domain.UpstreamCatalogTariffs{FetchedAt: fetchedAt}, maxAge: 24 * time.Hour, want: get_catalog_freshness.Output{FetchedAt: fetchedAt.Unix(), MaxAgeHours: 24}},
		{name: "older than max age is stale", tariffs: &domain.UpstreamCatalogTariffs{FetchedAt: fetchedAt}, maxAge: 23 * time.Hour, want: get_catalog_freshness.Output{FetchedAt: fetchedAt.Unix(), MaxAgeHours: 23, Stale: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usecase := get_catalog_freshness.NewUsecase(publishedTariffs{tc.tariffs}, func() time.Time { return now }, func() time.Duration { return tc.maxAge })
			assert.Equal(t, tc.want, usecase.Execute())
		})
	}
}
