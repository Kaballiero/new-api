package get_personal_pricing

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

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

type fakeUsers map[int]string

func (u fakeUsers) UserGroup(id int) (string, error) { return u[id], nil }

type fakeCache struct{}

func (fakeCache) Get(string) (domain.ConsumerPricingResponse, bool) {
	return domain.ConsumerPricingResponse{}, false
}
func (fakeCache) Put(string, domain.ConsumerPricingResponse) {}

var testCapabilities = pricingadapter.Capabilities()

// userOverride routes requests of user 1 to provider "a" and of everyone else
// to provider "b", like a channel override with a user_id condition.
const userOverride = `{"operations":[{"mode":"set","path":"provider.only","value":["a"],"conditions":[{"path":"user_id","mode":"full","value":1}]}]}`

// pricingFixture prices models through a projection that counts FX
// conversions: one per model and group in every projection of the catalogue.
type pricingFixture struct {
	reader        *fakeReader
	projection    *domain.Projection
	conversions   *atomic.Int64
	lists         *pricingadapter.ProjectionCache[domain.EffectivePricingList]
	pendingBuilds []func()
	usecase       Usecase
}

func newPricingFixture(pricing ...domain.Pricing) *pricingFixture {
	fixture := &pricingFixture{
		reader: &fakeReader{
			pricing:  append([]domain.Pricing{textModel("beta", 2), textModel("alpha", 3), textModel("gamma", 1)}, pricing...),
			source:   "snapshot-1",
			fxFactor: 1,
			ratios:   map[string]float64{"default": 1, "vip": 0.5},
		},
		conversions: &atomic.Int64{},
	}
	fixture.projection = domain.NewProjection(domain.ProjectionRuntime{Capabilities: testCapabilities, ApplyFX: func(ratio, factor float64) (float64, error) {
		fixture.conversions.Add(1)
		return ratio * factor, nil
	}, ApplyOverride: func(_ []byte, overrides map[string]any, ctx domain.OverrideContext) ([]byte, error) {
		provider := "a"
		if strings.Contains(fmt.Sprint(overrides), "user_id") && ctx.UserID != 1 {
			provider = "b"
		}
		return []byte(`{"model":"` + ctx.Upstream + `","provider":{"only":["` + provider + `"]}}`), nil
	}})
	fixture.lists = &pricingadapter.ProjectionCache[domain.EffectivePricingList]{UserListLimit: 2, Rebuild: func(build func()) {
		fixture.pendingBuilds = append(fixture.pendingBuilds, build)
	}}
	users := fakeUsers{1: "default", 2: "default", 3: "default", 4: "vip"}
	fixture.usecase = NewUsecase(testCapabilities, fixture.reader, fixture.projection, fakeCache{}, fixture.lists, users)
	return fixture
}

func textModel(name string, inputUSD float64) domain.Pricing {
	return domain.Pricing{ModelName: name, CatalogModality: "TEXT", EnableGroup: []string{"all"}, CatalogOffersByGroup: map[string][]domain.CatalogOffer{"all": {{ID: name, Operation: "text", Mode: "sync", MainComponent: "input", Prices: []domain.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: &inputUSD}}}}}}
}

// routedModel is priced only by an upstream offer of provider "a" behind a
// channel with the given parameter override.
func routedModel(name, override string) domain.Pricing {
	tag, amount, billable, eligible := "a", "4", "input_tokens", true
	rate := domain.SourceRate{Parameter: "prompt", Billable: &billable, AmountUSD: &amount, Unit: "million_tokens", Quantity: "1000000"}
	rate.Normalization.Comparable, rate.Normalization.Effective = true, true
	offer := domain.SourceOffer{ID: "offer-a", Provider: &domain.SourceProvider{Tag: &tag}, Eligibility: &domain.SourceEligibility{Eligible: eligible}, Components: []domain.SourceRate{rate}}
	route := domain.UpstreamCatalogRoute{Operation: "text", Mode: "sync", Endpoint: "openai", ModelID: "vendor/" + name, DocumentHash: name, Document: &domain.SourceDocument{ID: "vendor/" + name, Offers: []domain.SourceOffer{offer}}, ParamOverride: &override}
	return domain.Pricing{ModelName: name, CatalogModality: "TEXT", EnableGroup: []string{"all"}, UpstreamCatalogRoutesByGroup: map[string][]domain.UpstreamCatalogRoute{"all": {route}}}
}

// uncachedPage builds the page of the user from the reader's current inputs
// without a cache, as every personal page was built before the cache.
func (f *pricingFixture) uncachedPage(t *testing.T, userID int, group string, query domain.EffectivePricingPageQuery) domain.EffectivePricingPageData {
	t.Helper()
	ctx := domain.EffectivePricingPageContext{Policy: f.projection, UserID: userID, Metadata: domain.ConsumerPricingResponse{SchemaVersion: "v2", UserGroup: group, Currency: "RUB", Data: []domain.ConsumerPricingModel{}}, FxFactor: f.reader.fxFactor, Pricing: f.reader.pricing, UsableGroups: map[string]string{group: group}, GroupRatios: map[string]float64{group: f.reader.ratios[group]}}
	var err error
	ctx.Metadata.CatalogRevision, err = domain.ConsumerPricingRevision(testCapabilities, f.reader.source, ctx)
	require.NoError(t, err)
	return f.projection.BuildEffectivePricingPage(ctx, query, ctx.Project)
}

func (f *pricingFixture) page(t *testing.T, userID int, query domain.EffectivePricingPageQuery) domain.EffectivePricingPageData {
	t.Helper()
	query.Personal = true
	output, err := f.usecase.Execute(Input{UserID: userID, Page: true, Query: query})
	require.NoError(t, err)
	require.NotNil(t, output.Page)
	return *output.Page
}

var priceQuery = domain.EffectivePricingPageQuery{Personal: true, Limit: 20, OrderBy: "INPUT_PRICE"}

func TestPriceKeyedPagesReuseOneProjectedList(t *testing.T) {
	fixture := newPricingFixture()
	creatorID := 7
	queries := []domain.EffectivePricingPageQuery{
		{Limit: 20, OrderBy: "INPUT_PRICE"},
		{Limit: 2, Offset: 1, OrderBy: "INPUT_PRICE", Direction: "desc"},
		{Limit: 20, OrderBy: "OUTPUT_PRICE"},
		{Limit: 20, OrderBy: "NAME", PricingUnit: "TOKENS"},
		{Limit: 20, OrderBy: "INPUT_PRICE", PricingUnit: "CALLS"},
		{Limit: 2, Offset: 2, OrderBy: "NAME"},
		{Limit: 20, OrderBy: "NAME", Direction: "desc", Search: "ALP"},
		{Limit: 20, OrderBy: "INPUT_PRICE", Modality: "IMAGE"},
		{Limit: 20, OrderBy: "INPUT_PRICE", CreatorID: &creatorID},
	}
	var expected []domain.EffectivePricingPageData
	for _, query := range queries {
		query.Personal = true
		expected = append(expected, fixture.uncachedPage(t, 1, "default", query))
	}
	conversionsPerProjection := int64(len(fixture.reader.pricing))
	fixture.conversions.Store(0)

	for index, query := range queries {
		assert.Equal(t, expected[index], fixture.page(t, 1, query), "%+v", query)
	}

	assert.Equal(t, conversionsPerProjection, fixture.conversions.Load(), "the catalogue is projected once")
	assert.Empty(t, fixture.pendingBuilds)
}

func TestNamePagesWithoutListProjectOnlyTheirModels(t *testing.T) {
	fixture := newPricingFixture()
	query := domain.EffectivePricingPageQuery{Personal: true, Limit: 1, OrderBy: "NAME"}
	expected := fixture.uncachedPage(t, 1, "default", query)
	fixture.conversions.Store(0)

	assert.Equal(t, expected, fixture.page(t, 1, query))
	assert.Equal(t, int64(1), fixture.conversions.Load(), "only the page's model is projected")

	fixture.page(t, 1, priceQuery)
	fixture.conversions.Store(0)
	assert.Equal(t, expected, fixture.page(t, 1, query))
	assert.Zero(t, fixture.conversions.Load(), "a cached list serves name pages too")
}

func TestChangedInputsServeStaleListUntilOneRebuildFinishes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*fakeReader)
	}{
		{"catalogue snapshot", func(r *fakeReader) { r.pricing, r.source = append(r.pricing, textModel("delta", 4)), "snapshot-2" }},
		{"group ratio", func(r *fakeReader) { r.ratios["default"] = 2 }},
		{"FX factor", func(r *fakeReader) { r.fxFactor = 1.5 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPricingFixture()
			stale := fixture.page(t, 1, priceQuery)

			tc.change(fixture.reader)
			fresh := fixture.uncachedPage(t, 1, "default", priceQuery)
			require.NotEqual(t, stale, fresh)
			assert.Equal(t, stale, fixture.page(t, 1, priceQuery))
			assert.Equal(t, stale, fixture.page(t, 2, priceQuery))
			require.Len(t, fixture.pendingBuilds, 1, "one rebuild per list")

			// A client that saw the fresh revision is told the served list is older.
			_, err := fixture.usecase.Execute(Input{UserID: 1, Page: true, Query: domain.EffectivePricingPageQuery{Personal: true, Limit: 20, OrderBy: "INPUT_PRICE", ExpectedRevision: fresh.CatalogRevision}})
			require.ErrorIs(t, err, domain.ErrRevisionMismatch)

			fixture.pendingBuilds[0]()
			assert.Equal(t, fresh, fixture.page(t, 1, priceQuery))
			assert.Len(t, fixture.pendingBuilds, 1)
		})
	}
}

func TestUsersShareListsOnlyWhenOverridesIgnoreUserID(t *testing.T) {
	for _, tc := range []struct {
		name        string
		override    string
		projections int64
	}{
		{"no user_id condition", `{"operations":[{"mode":"set","path":"provider.only","value":["a"],"conditions":[{"path":"user_group","mode":"full","value":"default"}]}]}`, 1},
		{"user_id condition", userOverride, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPricingFixture(routedModel("routed", tc.override))
			expected := map[int]domain.EffectivePricingPageData{}
			for _, user := range []int{1, 2} {
				expected[user] = fixture.uncachedPage(t, user, "default", priceQuery)
			}
			conversionsPerProjection := int64(len(fixture.reader.pricing))
			fixture.conversions.Store(0)

			for _, user := range []int{1, 2, 1, 2} {
				assert.Equal(t, expected[user], fixture.page(t, user, priceQuery), "user %d", user)
			}

			assert.Equal(t, tc.projections*conversionsPerProjection, fixture.conversions.Load())
		})
	}
}

func TestUserListsAreSeparateAndCapped(t *testing.T) {
	fixture := newPricingFixture(routedModel("routed", userOverride))
	require.NotEqual(t, fixture.uncachedPage(t, 1, "default", priceQuery).Data, fixture.uncachedPage(t, 2, "default", priceQuery).Data, "the override gives user 1 a provider other users lack")
	conversionsPerProjection := int64(len(fixture.reader.pricing))

	for _, tc := range []struct {
		user      int
		projected bool
	}{
		{1, true},
		{2, true},
		{1, false},
		{2, false},
		{3, true}, // the limit of 2 drops user 1, read least recently
		{2, false},
		{1, true},
	} {
		fixture.conversions.Store(0)
		page := fixture.page(t, tc.user, priceQuery)
		projections := fixture.conversions.Load()
		assert.Equal(t, fixture.uncachedPage(t, tc.user, "default", priceQuery), page, "user %d", tc.user)
		if tc.projected {
			assert.Equal(t, conversionsPerProjection, projections, "user %d: list built", tc.user)
		} else {
			assert.Zero(t, projections, "user %d: list cached", tc.user)
		}
	}
}

func TestPagesOfDifferentGroupsNeverShareLists(t *testing.T) {
	fixture := newPricingFixture()

	defaultPage := fixture.page(t, 1, priceQuery)
	vipPage := fixture.page(t, 4, priceQuery)

	assert.Equal(t, fixture.uncachedPage(t, 1, "default", priceQuery), defaultPage)
	assert.Equal(t, fixture.uncachedPage(t, 4, "vip", priceQuery), vipPage)
	assert.Equal(t, "vip", vipPage.UserGroup)
	assert.NotEqual(t, defaultPage.CatalogRevision, vipPage.CatalogRevision)
	require.Len(t, vipPage.Data, 3)
	assert.Equal(t, 100.0, *defaultPage.Data[0].GroupPrices[0].Operations[0].Prices[0].AmountRub, "gamma: 1 USD x 100 x 1")
	assert.Equal(t, 50.0, *vipPage.Data[0].GroupPrices[0].Operations[0].Prices[0].AmountRub, "gamma: 1 USD x 100 x 0.5")
}

func TestWarmUpBuildsOnlyTheSharedList(t *testing.T) {
	for _, tc := range []struct {
		name        string
		pricing     []domain.Pricing
		projections int64
	}{
		{"shared list", nil, 1},
		{"user_id condition", []domain.Pricing{routedModel("routed", userOverride)}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPricingFixture(tc.pricing...)
			conversionsPerProjection := int64(len(fixture.reader.pricing))

			require.NoError(t, fixture.usecase.WarmUp("default"))
			assert.Equal(t, tc.projections*conversionsPerProjection, fixture.conversions.Load(), "warm-up")

			fixture.conversions.Store(0)
			page := fixture.page(t, 2, priceQuery)
			assert.Equal(t, (1-tc.projections)*conversionsPerProjection, fixture.conversions.Load(), "first price page")
			assert.Equal(t, fixture.uncachedPage(t, 2, "default", priceQuery), page)
		})
	}
}
