package get_group_pricing

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReader serves one snapshot; every user group may use only itself at the
// ratio configured for it.
type fakeReader struct {
	pricing  []domain.Pricing
	source   string
	fxFactor float64
	ratios   map[string]float64
	billable map[string]bool
}

func (r *fakeReader) Snapshot() ([]domain.Pricing, []domain.PricingVendor, string, error) {
	return r.pricing, nil, r.source, nil
}
func (r *fakeReader) FX() (domain.BillingFXBasis, error) {
	return domain.BillingFXBasis{Factor: r.fxFactor}, nil
}
func (*fakeReader) AutoGroups(string) []string { return nil }
func (*fakeReader) UsableGroups(group string) map[string]string {
	return map[string]string{group: group}
}
func (r *fakeReader) GroupRatio(group, _ string) float64 { return r.ratios[group] }
func (r *fakeReader) Billable([]domain.Pricing, string, string) map[string]bool {
	return r.billable
}

type fakeGroups map[string]bool

func (g fakeGroups) ContainsGroup(group string) bool { return g[group] }

type fakeCache struct{ puts int }

func (*fakeCache) Get(string) (domain.ConsumerPricingResponse, bool) {
	return domain.ConsumerPricingResponse{}, false
}
func (c *fakeCache) Put(string, domain.ConsumerPricingResponse) { c.puts++ }

var testCapabilities = domain.Capabilities{Codec: domain.JSONCodec{Marshal: common.Marshal}}

// pricingFixture prices models through a projection that counts FX
// conversions: one per model and group in every projection of the catalogue.
type pricingFixture struct {
	reader        *fakeReader
	projection    *domain.Projection
	conversions   *atomic.Int64
	pendingBuilds []func()
	usecase       Usecase
}

func newPricingFixture() *pricingFixture {
	fixture := &pricingFixture{
		reader: &fakeReader{
			pricing:  []domain.Pricing{textModel("beta", 2), textModel("alpha", 3), textModel("gamma", 1)},
			source:   "snapshot-1",
			fxFactor: 1,
			ratios:   map[string]float64{"default": 1, "vip": 0.5},
		},
		conversions: &atomic.Int64{},
	}
	fixture.projection = domain.NewProjection(domain.ProjectionRuntime{Capabilities: testCapabilities, ApplyFX: func(ratio, factor float64) (float64, error) {
		fixture.conversions.Add(1)
		return ratio * factor, nil
	}})
	lists := &pricingadapter.ProjectionCache[domain.EffectivePricingList]{Rebuild: func(build func()) {
		fixture.pendingBuilds = append(fixture.pendingBuilds, build)
	}}
	fixture.usecase = NewUsecase(testCapabilities, fixture.reader, fixture.projection, &fakeCache{}, lists, fakeGroups{"default": true, "vip": true})
	return fixture
}

func textModel(name string, inputUSD float64) domain.Pricing {
	return domain.Pricing{ModelName: name, CatalogModality: "TEXT", EnableGroup: []string{"all"}, CatalogOffersByGroup: map[string][]domain.CatalogOffer{"all": {{ID: name, Operation: "text", Mode: "sync", MainComponent: "input", Prices: []domain.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: &inputUSD}}}}}}
}

// uncachedPage builds the page from the reader's current inputs without a cache.
func (f *pricingFixture) uncachedPage(t *testing.T, group string, query domain.EffectivePricingPageQuery) domain.EffectivePricingPageData {
	t.Helper()
	ctx := domain.EffectivePricingPageContext{Policy: f.projection, Metadata: domain.ConsumerPricingResponse{SchemaVersion: "v2", UserGroup: group, Currency: "RUB", Data: []domain.ConsumerPricingModel{}}, FxFactor: f.reader.fxFactor, Pricing: f.reader.pricing, UsableGroups: map[string]string{group: group}, GroupRatios: map[string]float64{group: f.reader.ratios[group]}, UsingGroup: group, Billable: f.reader.billable}
	var err error
	ctx.Metadata.CatalogRevision, err = domain.ConsumerPricingRevision(testCapabilities, f.reader.source, ctx)
	require.NoError(t, err)
	return f.projection.BuildEffectivePricingPage(ctx, query, ctx.Project)
}

func (f *pricingFixture) page(t *testing.T, group string, query domain.EffectivePricingPageQuery) domain.EffectivePricingPageData {
	t.Helper()
	output, err := f.usecase.Execute(Input{Group: group, Page: true, Query: query})
	require.NoError(t, err)
	require.NotNil(t, output.Page)
	return *output.Page
}

func TestExecuteRejectsGroupsAndStaleRevisions(t *testing.T) {
	cache := &fakeCache{}
	reader := &fakeReader{source: "source", fxFactor: 1, ratios: map[string]float64{"default": 1}}
	usecase := NewUsecase(testCapabilities, reader, domain.NewProjection(domain.ProjectionRuntime{Capabilities: testCapabilities}), cache, &pricingadapter.ProjectionCache[domain.EffectivePricingList]{}, fakeGroups{"default": true, "auto": true})

	for _, group := range []string{"auto", "vip"} {
		_, err := usecase.Execute(Input{Group: group})
		require.ErrorIs(t, err, domain.ErrInvalidUserGroup, group)
		assert.EqualError(t, err, "invalid user pricing group")
	}

	_, err := usecase.Execute(Input{Page: true, UsingGroup: "vip"})
	require.ErrorIs(t, err, domain.ErrInvalidQuery)
	assert.ErrorIs(t, err, domain.ErrPricingUsingGroup)
	assert.EqualError(t, err, "invalid using_group")

	_, err = usecase.Execute(Input{Page: true, Query: domain.EffectivePricingPageQuery{ExpectedRevision: "stale", Limit: 20}})
	require.ErrorIs(t, err, domain.ErrRevisionMismatch)

	// The full catalogue of an anonymous group read is cached; using_group
	// is ignored outside page mode.
	output, err := usecase.Execute(Input{UsingGroup: "vip"})
	require.NoError(t, err)
	require.NotNil(t, output.Full)
	assert.Equal(t, "default", output.Full.UserGroup)
	assert.Equal(t, 1, cache.puts)
}

func TestPagesReuseOneProjectedListAcrossPagesAndFilters(t *testing.T) {
	fixture := newPricingFixture()
	creatorID := 7
	queries := []domain.EffectivePricingPageQuery{
		{Limit: 2, OrderBy: "NAME"},
		{Limit: 2, Offset: 2, OrderBy: "NAME"},
		{Limit: 20, OrderBy: "NAME", Direction: "desc"},
		{Limit: 20, OrderBy: "INPUT_PRICE"},
		{Limit: 20, OrderBy: "INPUT_PRICE", Direction: "desc", PricingUnit: "TOKENS"},
		{Limit: 20, OrderBy: "NAME", Search: "ALP"},
		{Limit: 20, OrderBy: "NAME", Modality: "IMAGE"},
		{Limit: 20, OrderBy: "NAME", CreatorID: &creatorID},
	}

	var expected []domain.EffectivePricingPageData
	for _, query := range queries {
		expected = append(expected, fixture.uncachedPage(t, "default", query))
	}
	uncachedConversions := fixture.conversions.Load()
	for index, query := range queries {
		assert.Equal(t, expected[index], fixture.page(t, "default", query), "%+v", query)
	}

	conversionsPerProjection := uncachedConversions / int64(len(queries))
	assert.Equal(t, conversionsPerProjection, fixture.conversions.Load()-uncachedConversions, "the catalogue is projected once")
	assert.Empty(t, fixture.pendingBuilds)
}

func TestChangedInputsServeStaleListUntilOneRebuildFinishes(t *testing.T) {
	query := domain.EffectivePricingPageQuery{Limit: 20, OrderBy: "INPUT_PRICE"}
	for _, tc := range []struct {
		name   string
		change func(*fakeReader)
	}{
		{"catalogue snapshot", func(r *fakeReader) { r.pricing, r.source = append(r.pricing, textModel("delta", 4)), "snapshot-2" }},
		{"group ratio", func(r *fakeReader) { r.ratios["default"] = 2 }},
		{"FX factor", func(r *fakeReader) { r.fxFactor = 1.5 }},
		{"billable models", func(r *fakeReader) { r.billable = map[string]bool{"alpha": true} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPricingFixture()
			stale := fixture.page(t, "default", query)

			tc.change(fixture.reader)
			fresh := fixture.uncachedPage(t, "default", query)
			require.NotEqual(t, stale, fresh)
			assert.Equal(t, stale, fixture.page(t, "default", query))
			assert.Equal(t, stale, fixture.page(t, "default", query))
			require.Len(t, fixture.pendingBuilds, 1, "one rebuild per tariff")

			fixture.pendingBuilds[0]()
			assert.Equal(t, fresh, fixture.page(t, "default", query))
			assert.Len(t, fixture.pendingBuilds, 1)
		})
	}
}

func TestConcurrentPagesOnColdCacheProjectOnce(t *testing.T) {
	fixture := newPricingFixture()
	query := domain.EffectivePricingPageQuery{Limit: 20, OrderBy: "NAME"}
	expected := fixture.uncachedPage(t, "default", query)
	conversionsPerProjection := fixture.conversions.Load()
	pages := make([]domain.EffectivePricingPageData, 8)
	var requests sync.WaitGroup
	for index := range pages {
		requests.Go(func() {
			output, err := fixture.usecase.Execute(Input{Group: "default", Page: true, Query: query})
			if assert.NoError(t, err) {
				pages[index] = *output.Page
			}
		})
	}
	requests.Wait()

	assert.Equal(t, 2*conversionsPerProjection, fixture.conversions.Load(), "the catalogue is projected once")
	for _, page := range pages {
		assert.Equal(t, expected, page)
	}
}

func TestPagesOfDifferentGroupsNeverShareLists(t *testing.T) {
	fixture := newPricingFixture()
	query := domain.EffectivePricingPageQuery{Limit: 20, OrderBy: "NAME"}

	defaultPage := fixture.page(t, "default", query)
	vipPage := fixture.page(t, "vip", query)

	assert.Equal(t, fixture.uncachedPage(t, "default", query), defaultPage)
	assert.Equal(t, fixture.uncachedPage(t, "vip", query), vipPage)
	assert.Equal(t, "vip", vipPage.UserGroup)
	assert.NotEqual(t, defaultPage.CatalogRevision, vipPage.CatalogRevision)
	require.Len(t, vipPage.Data, 3)
	assert.Equal(t, 300.0, *defaultPage.Data[0].GroupPrices[0].Operations[0].Prices[0].AmountRub, "alpha: 3 USD x 100 x 1")
	assert.Equal(t, 150.0, *vipPage.Data[0].GroupPrices[0].Operations[0].Prices[0].AmountRub, "alpha: 3 USD x 100 x 0.5")
	assert.Empty(t, fixture.pendingBuilds)
}
