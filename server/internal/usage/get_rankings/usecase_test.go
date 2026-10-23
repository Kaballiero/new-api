package get_rankings

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/usage/domain"
)

type rankingFixture struct {
	reads    [][2]int64
	buckets  [3]int64
	failure  error
	snapshot *RankingsResponse
	expires  time.Time
	writes   int
}

func (f *rankingFixture) GetRankingQuotaTotals(start, end int64) ([]domain.RankingQuotaTotal, error) {
	f.reads = append(f.reads, [2]int64{start, end})
	return []domain.RankingQuotaTotal{{ModelName: "alpha", TotalTokens: 10}}, f.failure
}

func (f *rankingFixture) GetRankingQuotaBuckets(start, end, size int64) ([]domain.RankingQuotaBucket, error) {
	f.buckets = [3]int64{start, end, size}
	return nil, nil
}

func (*rankingFixture) RankingMetadata() ([]domain.RankingVendor, []domain.RankingPrice) {
	return []domain.RankingVendor{{ID: 1, Name: "vendor", Icon: "icon"}},
		[]domain.RankingPrice{{ModelName: "alpha", VendorID: 1}}
}

func (f *rankingFixture) Get(string) (*RankingsResponse, time.Time, bool) {
	return f.snapshot, f.expires, f.snapshot != nil
}

func (f *rankingFixture) Put(_ string, data *RankingsResponse, expires time.Time) {
	f.snapshot, f.expires = data, expires
	f.writes++
}

func TestRankingsRangesAndCacheExpiry(t *testing.T) {
	for _, period := range []struct {
		name     string
		duration time.Duration
		bucket   int64
	}{
		{"today", 24 * time.Hour, 3600}, {"week", 7 * 24 * time.Hour, 86400},
		{"month", 30 * 24 * time.Hour, 86400}, {"year", 365 * 24 * time.Hour, 7 * 86400},
	} {
		t.Run(period.name, func(t *testing.T) {
			initial := time.Date(2024, 3, 1, 12, 34, 56, 0, time.UTC)
			now := initial
			fixture := &rankingFixture{}
			usecase := NewUsecase(fixture, fixture, fixture, func() time.Time { return now })
			result, err := usecase.Execute(period.name)
			require.NoError(t, err)
			require.Len(t, result.Models, 1)
			assert.Equal(t, "vendor", result.Models[0].Vendor)
			assert.Equal(t, "icon", result.Models[0].VendorIcon)
			start := initial.Add(-period.duration).Unix()
			assert.Equal(t, [][2]int64{{start, initial.Unix()}, {start - int64(period.duration/time.Second), start - 1}}, fixture.reads)
			assert.Equal(t, [3]int64{start, initial.Unix(), period.bucket}, fixture.buckets)
			assert.Equal(t, initial.Add(5*time.Minute), fixture.expires)
			cached, err := usecase.Execute(period.name)
			require.NoError(t, err)
			assert.Same(t, result, cached)
			assert.Len(t, fixture.reads, 2)
			now = fixture.expires
			_, err = usecase.Execute(period.name)
			require.NoError(t, err)
			assert.Len(t, fixture.reads, 4)
			assert.Equal(t, 2, fixture.writes)
		})
	}
}

func TestRankingsRejectInvalidPeriodAndDoNotCacheReadFailures(t *testing.T) {
	fixture := &rankingFixture{failure: errors.New("storage failure")}
	usecase := NewUsecase(fixture, fixture, fixture, time.Now)
	_, err := usecase.Execute("invalid")
	var period *domain.InvalidRankingPeriod
	require.ErrorAs(t, err, &period)
	assert.Equal(t, "invalid", period.Period)
	require.EqualError(t, err, "invalid ranking period: invalid")
	assert.Empty(t, fixture.reads)
	_, err = usecase.Execute("week")
	require.ErrorIs(t, err, fixture.failure)
	assert.Len(t, fixture.reads, 1)
	assert.Zero(t, fixture.writes)
}
