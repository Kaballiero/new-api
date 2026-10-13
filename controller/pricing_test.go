package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const prodFXFactor = 0.854594

func tieredPricing(expr string) model.Pricing {
	return model.Pricing{BillingMode: billing_setting.BillingModeTieredExpr, BillingExpr: expr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{expr: billingexpr.CatalogPrices(expr)}}
}

func float(v float64) *float64 { return &v }

func TestEffectiveGroupPricingTieredExpressionEmitsRoubleTiers(t *testing.T) {
	groupPrice := buildEffectiveGroupPricing(tieredPricing(`tier("base", p * 2 + c * 10 + cr * 0.2 + cc * 2.5 + cc1h * 4)`), "default", 1, prodFXFactor)

	assert.Equal(t, "formula", groupPrice.Status)
	assert.Empty(t, groupPrice.Limitations)
	assert.False(t, groupPrice.IsFree)
	require.Len(t, groupPrice.Tiers, 1)
	tier := groupPrice.Tiers[0]
	assert.Equal(t, "base", tier.Label)
	assert.Nil(t, tier.Condition)
	require.Len(t, tier.UnitPrices, 5)
	for i, expected := range []effectiveUnitPrice{
		{Component: "input", Unit: "million_tokens", AmountRub: 170.9188},
		{Component: "output", Unit: "million_tokens", AmountRub: 854.594},
		{Component: "cache_read", Unit: "million_tokens", AmountRub: 17.09188},
		{Component: "cache_write", Unit: "million_tokens", AmountRub: 213.6485},
		{Component: "cache_write_1h", Unit: "million_tokens", AmountRub: 341.8376},
	} {
		assert.Equal(t, expected.Component, tier.UnitPrices[i].Component)
		assert.Equal(t, expected.Unit, tier.UnitPrices[i].Unit)
		assert.InDelta(t, expected.AmountRub, tier.UnitPrices[i].AmountRub, 1e-9, expected.Component)
	}
}

func TestEffectiveGroupPricingRatioAndPerCallMirrorUnitPrices(t *testing.T) {
	ratio := buildEffectiveGroupPricing(model.Pricing{ModelRatio: 5, CompletionRatio: 5, CacheRatio: float(0.1)}, "default", 1, prodFXFactor)
	require.Len(t, ratio.UnitPrices, 3)
	assert.InDelta(t, 854.594, ratio.UnitPrices[0].AmountRub, 1e-9)
	require.Len(t, ratio.Tiers, 1)
	assert.Equal(t, effectivePricingTier{UnitPrices: ratio.UnitPrices}, ratio.Tiers[0])
	assert.False(t, ratio.IsFree)

	perCall := buildEffectiveGroupPricing(model.Pricing{QuotaType: 1, ModelPrice: 0.5}, "default", 1, prodFXFactor)
	require.Len(t, perCall.Tiers, 1)
	assert.Equal(t, effectivePricingTier{UnitPrices: perCall.UnitPrices}, perCall.Tiers[0])
	assert.Equal(t, []string{"provider-specific multipliers may apply for some request parameters"}, perCall.Limitations)
}

func TestEffectiveGroupPricingConditions(t *testing.T) {
	t.Run("context length bounds are exclusive below and inclusive above", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`len <= 32000 ? tier("0_32k", p * 1) : len <= 256000 ? tier("32k_256k", p * 2) : tier("256k_plus", p * 3)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 3)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "context_length", Unit: "token", MaxValue: float(32000)}, groupPrice.Tiers[0].Condition)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "context_length", Unit: "token", MinValue: float(32000), MaxValue: float(256000)}, groupPrice.Tiers[1].Condition)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "context_length", Unit: "token", MinValue: float(256000)}, groupPrice.Tiers[2].Condition)
	})
	t.Run("strict comparisons shift by one token", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`p < 1000 ? tier("small", p * 1) : tier("large", p * 2)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 2)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "input_tokens", Unit: "token", MaxValue: float(999)}, groupPrice.Tiers[0].Condition)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "input_tokens", Unit: "token", MinValue: float(999)}, groupPrice.Tiers[1].Condition)
	})
	t.Run("hour guards become time windows and their complement", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`(hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10) ? tier("peak", p * 1.32 + c * 3.96 + cr * 0.044) : tier("off_peak", p * 0.66 + c * 1.98 + cr * 0.022)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 2)
		assert.Equal(t, "peak", groupPrice.Tiers[0].Label)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "time_of_day", TimeZone: "UTC", TimeWindows: []effectivePricingTimeWindow{{1, 4}, {6, 10}}}, groupPrice.Tiers[0].Condition)
		assert.Equal(t, "off_peak", groupPrice.Tiers[1].Label)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "time_of_day", TimeZone: "UTC", TimeWindows: []effectivePricingTimeWindow{{0, 1}, {4, 6}, {10, 24}}}, groupPrice.Tiers[1].Condition)
		assert.InDelta(t, 112.806408, groupPrice.Tiers[0].UnitPrices[0].AmountRub, 1e-9)
		assert.InDelta(t, 56.403204, groupPrice.Tiers[1].UnitPrices[0].AmountRub, 1e-9)
	})
	t.Run("unclassifiable guard is reported as other", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`len == 4096 ? tier("exact", p * 1) : tier("rest", p * 2)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 2)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "other"}, groupPrice.Tiers[0].Condition)
	})
}

func TestEffectiveGroupPricingComponentPresence(t *testing.T) {
	t.Run("referenced zero is free, unreferenced is absent", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`p * 0 + c * 0`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 1)
		assert.Empty(t, groupPrice.Tiers[0].Label)
		assert.Equal(t, []effectiveUnitPrice{{Component: "input", Unit: "million_tokens"}, {Component: "output", Unit: "million_tokens"}}, groupPrice.Tiers[0].UnitPrices)
		assert.True(t, groupPrice.IsFree)
	})
	t.Run("component referenced by one tier is listed at zero in the others", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`len <= 1000 ? tier("a", p * 1 + cr * 0.5) : tier("b", p * 2)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 2)
		require.Len(t, groupPrice.Tiers[1].UnitPrices, 2)
		assert.Equal(t, "cache_read", groupPrice.Tiers[1].UnitPrices[1].Component)
		assert.Zero(t, groupPrice.Tiers[1].UnitPrices[1].AmountRub)
	})
	t.Run("variable outside the vocabulary is passed through verbatim", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`tier("base", p * 1 + len * 0.1)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 1)
		assert.Equal(t, []string{"input", "len"}, []string{groupPrice.Tiers[0].UnitPrices[0].Component, groupPrice.Tiers[0].UnitPrices[1].Component})
	})
}

func TestEffectiveGroupPricingRefusesUnpriceableExpressions(t *testing.T) {
	for name, expr := range map[string]string{
		"non-linear body":     `tier("base", max(p, c) * 2)`,
		"summed tiers":        `tier("a", p * 2) + tier("b", c * 3)`,
		"one non-linear tier": `len <= 1000 ? tier("a", p * 1) : tier("b", ceil(p) * 2)`,
	} {
		t.Run(name, func(t *testing.T) {
			groupPrice := buildEffectiveGroupPricing(tieredPricing(expr), "default", 1, prodFXFactor)
			assert.Equal(t, "formula", groupPrice.Status)
			assert.NotNil(t, groupPrice.Formula)
			assert.Equal(t, []effectivePricingTier{}, groupPrice.Tiers)
			assert.False(t, groupPrice.IsFree)
		})
	}
}

func TestEffectiveGroupPricingLegacyAudioOutputUsesAudioInputBasis(t *testing.T) {
	for _, tc := range []struct {
		name       string
		audioRatio *float64
		want       float64
	}{
		{name: "configured audio input ratio", audioRatio: float(4), want: 9600},
		{name: "missing audio input ratio defaults to one", want: 2400},
		{name: "explicit zero remains free", audioRatio: float(0), want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			price := buildEffectiveGroupPricing(model.Pricing{
				ModelRatio: 2, CompletionRatio: 7,
				AudioRatio: tc.audioRatio, AudioCompletionRatio: float(3),
			}, "paid", 4, 2)
			require.NotEmpty(t, price.UnitPrices)
			output := price.UnitPrices[len(price.UnitPrices)-1]
			assert.Equal(t, "audio_output", output.Component)
			assert.Equal(t, "million_tokens", output.Unit)
			assert.Equal(t, tc.want, output.AmountRub)
			require.Len(t, price.Tiers, 1)
			assert.Equal(t, price.UnitPrices, price.Tiers[0].UnitPrices)
		})
	}
}

func TestEffectiveGroupPricingExpressionAudioPricesIgnoreLegacyRatios(t *testing.T) {
	item := tieredPricing(`tier("audio", p * 2 + c * 7 + ai * 11 + ao * 13)`)
	item.ModelRatio = 50
	item.CompletionRatio = 60
	item.AudioRatio = float(70)
	item.AudioCompletionRatio = float(80)
	price := buildEffectiveGroupPricing(item, "paid", 4, 2)
	require.NotNil(t, price.Formula)
	assert.Equal(t, item.BillingExpr, price.Formula.Expression)
	assert.Empty(t, price.UnitPrices)
	require.Len(t, price.Tiers, 1)
	require.Len(t, price.Tiers[0].UnitPrices, 4)
	for i, expected := range []effectiveUnitPrice{
		{Component: "input", Unit: "million_tokens", AmountRub: 400},
		{Component: "output", Unit: "million_tokens", AmountRub: 1400},
		{Component: "audio_input", Unit: "million_tokens", AmountRub: 2200},
		{Component: "audio_output", Unit: "million_tokens", AmountRub: 2600},
	} {
		actual := price.Tiers[0].UnitPrices[i]
		assert.Equal(t, expected.Component, actual.Component)
		assert.Equal(t, expected.Unit, actual.Unit)
		assert.InDelta(t, expected.AmountRub, actual.AmountRub, 1e-9)
	}
}

func pricingPageFixture() effectivePricingPageContext {
	context := effectivePricingPageContext{
		metadata: consumerPricingResponse{SchemaVersion: "v2", UserGroup: "default", Currency: "RUB", AutoGroups: []string{}}, fxFactor: 1,
		usableGroups: map[string]string{"default": "Default", "premium": "Premium"},
		groupRatios:  map[string]float64{"default": 1, "premium": 2},
		vendors:      []model.PricingVendor{{ID: 1, Name: " OpenAI "}, {ID: 2, Name: "Anthropic"}, {ID: 3, Name: " "}, {ID: 0, Name: "omitted"}},
		pricing: []model.Pricing{
			{ModelName: "z-text", VendorID: 2, EnableGroup: []string{"default", "premium"}, ModelRatio: 2, CompletionRatio: 2, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}},
			{ModelName: "vendor/gpt-image-1", VendorID: 1, EnableGroup: []string{"default"}, QuotaType: 1, ModelPrice: 1, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}},
			{ModelName: "a-text", VendorID: 3, EnableGroup: []string{"default"}, ModelRatio: 1, CompletionRatio: 3, SupportedEndpointTypes: []constant.EndpointType{}},
			{ModelName: "private", VendorID: 2, EnableGroup: []string{"unusable"}, SupportedEndpointTypes: []constant.EndpointType{}},
			{ModelName: "premium-only", VendorID: 1, EnableGroup: []string{"premium"}, ModelRatio: 0.5, SupportedEndpointTypes: []constant.EndpointType{}},
		},
	}
	for index := range context.pricing {
		item := &context.pricing[index]
		item.PriceConfigured = true
		endpoint := constant.EndpointTypeOpenAI
		if item.QuotaType == 1 {
			endpoint = constant.EndpointTypeImageGeneration
		}
		item.BillingSourcesByGroup = map[string]map[constant.EndpointType][]string{}
		for _, group := range item.EnableGroup {
			item.BillingSourcesByGroup[group] = map[constant.EndpointType][]string{endpoint: {model.BillingSourceConfiguredTariff}}
		}
	}
	return context
}

func TestEffectivePricingPageSelectionAndProjectionBound(t *testing.T) {
	context := pricingPageFixture()
	query, err := parseEffectivePricingPageQuery(url.Values{"limit": {"1"}, "offset": {"1"}}, true)
	require.NoError(t, err)
	for range 2 {
		calls := 0
		page := buildEffectivePricingPage(context, query, func(item model.Pricing) consumerPricingModel { calls++; return context.project(item) })
		assert.Equal(t, 4, page.TotalCount)
		require.Len(t, page.Data, 1)
		assert.Equal(t, "premium-only", page.Data[0].ModelName)
		assert.Equal(t, 1, calls)
		assert.Equal(t, []effectivePricingCreator{{ID: 2, Name: "Anthropic"}, {ID: 1, Name: "OpenAI"}}, page.Creators)
	}
	for _, values := range []url.Values{{"limit": {"0"}}, {"offset": {"9007199254740991"}}} {
		query, err = parseEffectivePricingPageQuery(values, true)
		require.NoError(t, err)
		page := buildEffectivePricingPage(context, query, func(model.Pricing) consumerPricingModel {
			t.Fatal("empty page projected tariffs")
			return consumerPricingModel{}
		})
		assert.Equal(t, 4, page.TotalCount)
		assert.Empty(t, page.Data)
		assert.NotNil(t, page.Data)
	}
	query, err = parseEffectivePricingPageQuery(url.Values{"search": {"  GPT-IMAGE  "}, "modality": {"IMAGE"}, "creator_id": {"1"}}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "vendor/gpt-image-1", page.Data[0].ModelName)
	assert.Len(t, page.Creators, 2)
	query.creatorID = new(int)
	assert.Empty(t, buildEffectivePricingPage(context, query, context.project).Data)
}

func TestEffectivePricingPagePublicVisibilityAndSimpleWire(t *testing.T) {
	context := pricingPageFixture()
	context.usingGroup = "default"
	context.usableGroups = map[string]string{"default": "Default"}
	query, err := parseEffectivePricingPageQuery(url.Values{}, false)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Len(t, page.Data, 3)
	for _, row := range page.Data {
		require.Len(t, row.GroupPrices, 1)
		assert.Equal(t, "default", row.GroupPrices[0].UsingGroup)
	}
	encoded, err := common.Marshal(page)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"operations"`)
	for _, removed := range []string{"formula", "tiers", "operation_tariffs", "accounting", "billing_sources", "effective_billing_ratio"} {
		assert.NotContains(t, string(encoded), removed)
	}
}

func TestEffectivePricingPagePersonalPricesAndUnit(t *testing.T) {
	context := pricingPageFixture()
	query, err := parseEffectivePricingPageQuery(url.Values{"order_by": {"INPUT_PRICE"}, "direction": {"desc"}}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	names := []string{}
	for _, item := range page.Data {
		names = append(names, item.ModelName)
	}
	assert.Equal(t, []string{"z-text", "a-text", "premium-only", "vendor/gpt-image-1"}, names)
	require.Len(t, page.Data[0].GroupPrices, 2)
	query.direction = "asc"
	page = buildEffectivePricingPage(context, query, context.project)
	names = nil
	for _, item := range page.Data {
		names = append(names, item.ModelName)
	}
	assert.Equal(t, []string{"a-text", "premium-only", "z-text", "vendor/gpt-image-1"}, names)
	query.pricingUnit = "TOKENS"
	query.limit = 0
	calls := 0
	page = buildEffectivePricingPage(context, query, func(item model.Pricing) consumerPricingModel { calls++; return context.project(item) })
	assert.Equal(t, 3, page.TotalCount)
	assert.Empty(t, page.Data)
	assert.Equal(t, 4, calls)
	prices := []consumerUnitPrice{{Component: "input", Unit: "million_tokens", AmountRub: float(3)}}
	unit, price := effectivePricingPageKeys(consumerPricingModel{GroupPrices: []consumerGroupPricing{{Operations: []consumerPricingOperation{{Operation: "text", Mode: "batch", Prices: []consumerUnitPrice{{Component: "base", Unit: "call", AmountRub: float(1)}}}, {Operation: "text", Mode: "sync", Prices: prices}}}}}, "INPUT_PRICE")
	assert.Equal(t, "TOKENS", unit)
	require.NotNil(t, price)
	assert.Equal(t, 3.0, *price)
	unit, price = effectivePricingPageKeys(consumerPricingModel{}, "INPUT_PRICE")
	assert.Equal(t, "UNCONFIGURED", unit)
	assert.Nil(t, price)
}

func TestEffectivePricingPageValidationAndMetadata(t *testing.T) {
	for _, raw := range []string{"limit=101", "limit=-1", "limit=", "limit=1.2", "limit=1&limit=2", "offset=9007199254740992", "offset=-1", "offset=99999999999999999999", "creator_id=2147483648", "creator_id=x", "creator_id=1&creator_id=1", "modality=audio", "modality=TEXT&modality=IMAGE", "search=a&search=b", "pricing_unit=TASK", "order_by=PRICE", "direction=up", "group=default", "using_group=default"} {
		t.Run(raw, func(t *testing.T) {
			values, err := url.ParseQuery(raw)
			require.NoError(t, err)
			_, err = parseEffectivePricingPageQuery(values, true)
			require.Error(t, err)
		})
	}
	for _, raw := range []string{"limit=0", "limit=100", "creator_id=-2", "creator_id=0", "offset=9007199254740991", "search=%EF%BB%BFABC%EF%BB%BF"} {
		values, err := url.ParseQuery(raw)
		require.NoError(t, err)
		_, err = parseEffectivePricingPageQuery(values, true)
		require.NoError(t, err)
	}
	for _, test := range []struct {
		name      string
		endpoints []constant.EndpointType
		want      string
	}{
		{"vendor/sora-2", []constant.EndpointType{"openai"}, "VIDEO"},
		{"GPT-IMAGE-1", []constant.EndpointType{"OPENAI"}, "IMAGE"},
		{"vendor/text-embedding-3", []constant.EndpointType{"openai"}, "EMBEDDINGS"},
		{"tts-1", []constant.EndpointType{"openai"}, "OTHER"},
		{"gpt-4o-transcribe", []constant.EndpointType{"openai"}, "OTHER"},
		{"mixed", []constant.EndpointType{"audio", "rerank", "embeddings", "video", "image-generation"}, "IMAGE"},
		{"plain", nil, "TEXT"},
	} {
		assert.Equal(t, test.want, effectivePricingModality(model.Pricing{ModelName: test.name, SupportedEndpointTypes: test.endpoints}))
	}
	assert.Negative(t, compareEffectivePricingNames("A", "a"))
	assert.Negative(t, compareEffectivePricingNames("𐀀", "\ue000"))
	assert.Zero(t, compareEffectivePricingNames("İ", "İ"))
	assert.Equal(t, "abc", trimPricingText("\ufeffabc\ufeff"))
	assert.Equal(t, "\u0085", trimPricingText("\u0085"))
}

func TestEffectivePricingPageModeIsExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		query    string
		personal bool
		handled  bool
		status   int
	}{
		{"", false, false, 200}, {"?group=default", false, false, 200}, {"?group=ignored", true, false, 200},
		{"?offset=0", false, true, 400}, {"?search=", true, true, 400}, {"?using_group=default", false, true, 400},
		{"?limit=", false, true, 400}, {"?limit=1&limit=2", false, true, 400}, {"?limit=1&group=default", true, true, 400},
		{"?limit=1&using_group=default", true, true, 400}, {"?limit=1&group=auto", false, true, 400},
		{"?limit=1&using_group=not-authorized", false, true, 400},
		{"?limit=%ZZ", false, true, 400}, {"?limit=1;offset=0", false, true, 400},
		{"?limit=1&limit=%ZZ", false, true, 400}, {"?limit=1&modality=%ZZ", false, true, 400},
		{"?limit=1&group=%ZZ", true, true, 400}, {"?limit=1&using_group=%ZZ", true, true, 400},
		{"?limit=1&unknown=%ZZ", false, true, 400}, {"?%ZZ=1", false, true, 400},
		{"?%6cimit=%ZZ", false, true, 400}, {"?modality=%ZZ", true, true, 400},
		{"?group=%64efault", false, false, 200}, {"?unknown=%76alid", false, false, 200},
	} {
		t.Run(test.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/effective"+test.query, nil)
			assert.Equal(t, test.handled, routeEffectivePricingPage(context, test.personal))
			assert.Equal(t, test.status, recorder.Code)
			if test.handled {
				assert.Contains(t, recorder.Body.String(), `"success":false`)
			}
		})
	}
}

func TestEffectivePricingPageUnicodeGolden(t *testing.T) {
	context := pricingPageFixture()
	names := []string{"éclair", "Eclair", "eclair", "Å", "a", "Z", "same", "Ä", "Ö", "same"}
	context.pricing = nil
	context.vendors = nil
	for index, name := range names {
		id := index + 1
		context.vendors = append(context.vendors, model.PricingVendor{ID: id, Name: name})
		context.pricing = append(context.pricing, model.Pricing{ModelName: name, VendorID: id, EnableGroup: []string{"default"}})
	}
	query, err := parseEffectivePricingPageQuery(url.Values{}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	actual := []effectivePricingCreator{}
	actual = append(actual, page.Creators...)
	assert.Equal(t, []effectivePricingCreator{{ID: 5, Name: "a"}, {ID: 4, Name: "Å"}, {ID: 8, Name: "Ä"}, {ID: 3, Name: "eclair"}, {ID: 2, Name: "Eclair"}, {ID: 1, Name: "éclair"}, {ID: 9, Name: "Ö"}, {ID: 7, Name: "same"}, {ID: 10, Name: "same"}, {ID: 6, Name: "Z"}}, actual)
	models := []string{}
	for _, item := range page.Data {
		models = append(models, item.ModelName)
	}
	assert.Equal(t, []string{"a", "Eclair", "eclair", "same", "same", "Z", "Ä", "Å", "éclair", "Ö"}, models)
	query, err = parseEffectivePricingPageQuery(url.Values{"search": {"İ"}}, true)
	require.NoError(t, err)
	assert.Equal(t, "i\u0307", query.search)
	assert.Negative(t, compareEffectivePricingNames("ΟΣ", "οσ"))
}

func TestConsumerPricingKeepsCompleteCheapestOfferAndNativeUnits(t *testing.T) {
	offers := []model.CatalogOffer{
		{ID: "cheap-input", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: float(1)}, {Component: "output", Unit: "million_tokens", AmountUSD: float(9)}}},
		{ID: "cheap-output", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: float(7)}, {Component: "output", Unit: "million_tokens", AmountUSD: float(3)}, {Component: "cache_read", Unit: "million_tokens", AmountUSD: nil}}},
	}
	item := model.Pricing{ModelName: "coherent", EnableGroup: []string{"default", "premium"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"all": offers}}
	result := buildConsumerPricingModel(map[string]string{"default": "Default", "premium": "Premium"}, map[string]float64{"default": 1, "premium": 2}, .8, item)
	require.Len(t, result.GroupPrices, 2)
	for index, multiplier := range []float64{1, 2} {
		require.Len(t, result.GroupPrices[index].Operations, 1)
		prices := result.GroupPrices[index].Operations[0].Prices
		require.Len(t, prices, 3)
		assert.InDelta(t, 7*100*.8*multiplier, *prices[0].AmountRub, 1e-9)
		assert.InDelta(t, 3*100*.8*multiplier, *prices[1].AmountRub, 1e-9)
		assert.Nil(t, prices[2].AmountRub)
	}
	for _, unit := range []string{"image", "megapixel", "million_tokens", "second", "call"} {
		t.Run(unit, func(t *testing.T) {
			item.CatalogOffersByGroup = map[string][]model.CatalogOffer{"default": {{ID: "native", Operation: "image_generation", Mode: "sync", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: unit, AmountUSD: float(.03)}}}}}
			group := consumerOperations(item, "default", 1, true)
			require.Len(t, group, 1)
			require.Len(t, group[0].Prices, 1)
			assert.Equal(t, unit, group[0].Prices[0].Unit)
			assert.InDelta(t, 3, *group[0].Prices[0].AmountRub, 1e-9)
		})
	}
}

func TestConsumerPricingZeroUnknownAndIncompatibleUnits(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		offers []model.CatalogOffer
		want   *float64
	}{
		{"zero", []model.CatalogOffer{{ID: "free", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: float(0)}}}, {ID: "paid", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: float(1)}}}}, float(0)},
		{"unknown", []model.CatalogOffer{{ID: "unknown", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: nil}}}}, nil},
		{"different native units", []model.CatalogOffer{{ID: "image", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: float(1)}}}, {ID: "token", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "million_tokens", AmountUSD: float(.001)}}}}, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for index := range scenario.offers {
				scenario.offers[index].Operation = "image_generation"
				scenario.offers[index].Mode = "sync"
			}
			item := model.Pricing{CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": scenario.offers}}
			rows := consumerOperations(item, "default", 1, true)
			require.Len(t, rows, 1)
			if scenario.want == nil {
				assert.Empty(t, rows[0].Prices)
			} else {
				require.Len(t, rows[0].Prices, 1)
				require.NotNil(t, rows[0].Prices[0].AmountRub)
				assert.Equal(t, *scenario.want, *rows[0].Prices[0].AmountRub)
			}
		})
	}
}

func TestConsumerPricingBatchIsConfiguredAndIndependent(t *testing.T) {
	withTieredBillingConfig(t, nil, nil)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.batch_billing_expr": `{"catalog-batch":"tier(\"batch\",p * 4 + c * 7)"}`}))
	item := tieredPricing(`len <= 1000 ? tier("short",p * 8 + c * 2) : tier("long",p * 1 + c * 9)`)
	item.ModelName = "catalog-batch"
	batchExpression, _ := billing_setting.GetBatchBillingExpr(item.ModelName)
	item.ConfiguredPrices[batchExpression] = billingexpr.CatalogPrices(batchExpression)
	item.BillingSourcesByGroup = map[string]map[constant.EndpointType][]string{"default": {constant.EndpointTypeOpenAI: {model.BillingSourceConfiguredTariff}, model.PricingEndpointBatch: {model.BillingSourceConfiguredTariff}}}
	rows := consumerOperations(item, "default", 1, true)
	require.Len(t, rows, 2)
	assert.Equal(t, "batch", rows[0].Mode)
	assert.Equal(t, 400.0, *rows[0].Prices[0].AmountRub)
	assert.Equal(t, 700.0, *rows[0].Prices[1].AmountRub)
	assert.Equal(t, "sync", rows[1].Mode)
	assert.Equal(t, 800.0, *rows[1].Prices[0].AmountRub)
	assert.Equal(t, 200.0, *rows[1].Prices[1].AmountRub)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.batch_billing_expr": `{}`}))
	assert.Len(t, consumerOperations(item, "default", 1, true), 1)
	item.BillingExpr = `tier("cost",provider_cost)`
	rows = consumerOperations(item, "default", 1, true)
	require.Len(t, rows, 1)
	assert.Empty(t, rows[0].Prices)
}

func TestConsumerPricingSoraRetainsOperationPriceBasis(t *testing.T) {
	item := model.Pricing{ModelName: "sora-2", PriceConfigured: true, QuotaType: 1, ModelPrice: .3,
		BillingSourcesByGroup:        map[string]map[constant.EndpointType][]string{"default": {constant.EndpointTypeOpenAIVideo: {model.BillingSourceConfiguredTariff}}},
		LegacyVideoPriceBasisByGroup: map[string][]map[string]jsplugin.LegacyPriceBasis{"default": {{"text_to_video": {Unit: "second", UsageField: "seconds"}, "image_to_video": {Unit: "second", UsageField: "seconds"}, "remix": {Unit: "call"}}}},
	}
	rows := consumerOperations(item, "default", 1.2, true)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Len(t, row.Prices, 1)
		assert.Equal(t, 36.0, *row.Prices[0].AmountRub)
		if row.Operation == "remix" {
			assert.Equal(t, "call", row.Prices[0].Unit)
		} else {
			assert.Equal(t, "second", row.Prices[0].Unit)
		}
	}
}

func TestConsumerPricingPageUsesNativeChannelVideoPluginBinding(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousRegistry := jsplugin.DefaultRegistry
	previousMemoryCache := common.MemoryCacheEnabled
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		jsplugin.DefaultRegistry = previousRegistry
		common.MemoryCacheEnabled = previousMemoryCache
		model.InvalidatePricingCache()
	})
	source, err := os.ReadFile("../plugins/tasks/openrouter-video/plugin.js")
	require.NoError(t, err)
	_, err = jsplugin.DefaultRegistry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	mapping := `{"heygen-alias":"heygen/heygen-video-1"}`
	require.NoError(t, db.Create(&model.Channel{Id: 950, Type: constant.ChannelTypeOpenRouter, Status: 1, Key: "fixture", Models: "heygen-alias", ModelMapping: &mapping}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "heygen-alias", ChannelId: 950, Enabled: true}).Error)
	model.InitChannelCache()
	context := effectivePricingPageContext{metadata: consumerPricingResponse{SchemaVersion: "v2", UserGroup: "default", Currency: "RUB"}, fxFactor: 1, pricing: model.GetPricing(), usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1}, usingGroup: "default"}
	query, err := parseEffectivePricingPageQuery(url.Values{"limit": {"20"}, "modality": {"VIDEO"}}, false)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Equal(t, 1, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "heygen-alias", page.Data[0].ModelName)
	require.Len(t, page.Data[0].GroupPrices, 1)
	operations := make(map[string]consumerPricingOperation)
	for _, operation := range page.Data[0].GroupPrices[0].Operations {
		operations[operation.Operation] = operation
	}
	for operation, amountRub := range map[string]float64{"text_to_video": 2, "image_to_video": 4, "video_to_video": 4} {
		require.Contains(t, operations, operation)
		prices := operations[operation].Prices
		require.Len(t, prices, 1)
		assert.Equal(t, "second", prices[0].Unit)
		require.NotNil(t, prices[0].AmountRub)
		assert.Equal(t, amountRub, *prices[0].AmountRub)
	}
}

func TestPricingCatalogNormalizeCoherentEndpointVariants(t *testing.T) {
	body := []byte(`{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"a","context_length":100,"pricing":{"prompt":"0.000001","completion":"0.000009","input_cache_read":"0","overrides":[{"min_prompt_tokens":0,"prompt":"0.000007","completion":"0.000003"},{"min_prompt_tokens":200,"completion":"0.0000001"}]}},{"tag":"b","pricing":{"prompt":"0.0000001","completion":"0.000008"}}]}}`)
	offers, err := normalizeEndpointPricing("provider/model", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 2)
	reduced := selectPricingCatalogOffers(offers)
	require.Len(t, reduced, 1)
	selected := model.SelectCatalogOffer(reduced)
	require.NotNil(t, selected)
	prices := map[string]*float64{}
	for _, price := range selected.Prices {
		prices[price.Component] = price.AmountUSD
	}
	assert.InDelta(t, 7, *prices["input"], 1e-9)
	assert.InDelta(t, 3, *prices["output"], 1e-9)
	assert.Equal(t, 0.0, *prices["cache_read"])
}

func TestPricingCatalogReductionPreservesSelection(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		offers []model.CatalogOffer
	}{
		{"tie", []model.CatalogOffer{{ID: "z", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(1)}, {Component: "input", Unit: "million_tokens", AmountUSD: float(2)}}}, {ID: "a", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(1)}, {Component: "input", Unit: "million_tokens", AmountUSD: float(9)}}}}},
		{"zero_unknown", []model.CatalogOffer{{ID: "null", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens"}}}, {ID: "zero", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(0)}}}}},
		{"unlike_units", []model.CatalogOffer{{ID: "image", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: float(1)}}}, {ID: "mp", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "megapixel", AmountUSD: float(.01)}}}}},
		{"unknown_main", []model.CatalogOffer{{ID: "null", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: float(0)}}}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for index := range scenario.offers {
				scenario.offers[index].Operation = "text"
				scenario.offers[index].Mode = "sync"
			}
			before := model.SelectCatalogOffer(scenario.offers)
			after := model.SelectCatalogOffer(selectPricingCatalogOffers(scenario.offers))
			assert.Equal(t, before, after)
		})
	}
}

func TestPricingCatalogNativeImageUnitsAndUnknownVariants(t *testing.T) {
	for _, scenario := range []struct {
		name, body, unit string
		amount           *float64
	}{
		{"first_megapixel", `{"id":"provider/image","endpoints":[{"provider_tag":"native","pricing":[{"billable":"output_image","unit":"megapixel","cost_usd":0.03}]}]}`, "megapixel", float(.03)},
		{"token", `{"id":"provider/image","endpoints":[{"pricing":[{"billable":"output_image","unit":"token","cost_usd":0.00003}]}]}`, "million_tokens", float(30)},
		{"free", `{"id":"provider/image","endpoints":[{"pricing":[{"billable":"output_image","unit":"image","cost_usd":0}]}]}`, "image", float(0)},
		{"opaque_variant", `{"id":"provider/image","endpoints":[{"pricing":[{"billable":"output_image","unit":"image","cost_usd":0.001,"variant":"opaque"}]}]}`, "", nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			offers, err := normalizeImagePricing("provider/image", []byte(scenario.body))
			require.NoError(t, err)
			selected := model.SelectCatalogOffer(selectPricingCatalogOffers(offers))
			if scenario.amount == nil {
				assert.Nil(t, selected)
				return
			}
			require.NotNil(t, selected)
			require.Len(t, selected.Prices, 1)
			assert.Equal(t, scenario.unit, selected.Prices[0].Unit)
			assert.InDelta(t, *scenario.amount, *selected.Prices[0].AmountUSD, 1e-9)
		})
	}
	_, err := normalizeImagePricing("provider/image", []byte(`{"id":"provider/image","endpoints":[{"pricing":[{"billable":"output_image","unit":"image"}]}]}`))
	require.Error(t, err)
}

func TestPricingCatalogRefreshPartialFailureAndCancellation(t *testing.T) {
	sources := make([]pricingCatalogSource, 12)
	for index := range sources {
		sources[index].upstream = string(rune('a' + index))
	}
	var calls, published, active, maximum atomic.Int64
	gate := make(chan struct{})
	ready := make(chan struct{}, pricingCatalogWorkers)
	done := make(chan pricingCatalogRefreshSummary, 1)
	go func() {
		summary, err := refreshPricingCatalogSources(context.Background(), sources, func(ctx context.Context, source pricingCatalogSource) (model.PricingCatalogEntry, error) {
			count := active.Add(1)
			defer active.Add(-1)
			for previous := maximum.Load(); count > previous && !maximum.CompareAndSwap(previous, count); previous = maximum.Load() {
			}
			if calls.Add(1) <= pricingCatalogWorkers {
				ready <- struct{}{}
				<-gate
			}
			if source.upstream == "a" {
				return model.PricingCatalogEntry{}, errors.New("fixture failed metadata")
			}
			return model.PricingCatalogEntry{UpstreamModel: source.upstream}, nil
		}, func(entry model.PricingCatalogEntry) error { published.Add(1); return nil })
		assert.ErrorContains(t, err, "failed for 1 of 12 sources")
		done <- summary
	}()
	for range pricingCatalogWorkers {
		<-ready
	}
	close(gate)
	summary := <-done
	assert.Equal(t, 11, summary.Published)
	assert.Equal(t, 1, summary.Failed)
	assert.EqualValues(t, pricingCatalogWorkers, maximum.Load())
	assert.EqualValues(t, 11, published.Load())
	for _, failure := range []string{"fetch", "persist"} {
		t.Run("all_"+failure+"_fail", func(t *testing.T) {
			summary, err := refreshPricingCatalogSources(context.Background(), sources, func(context.Context, pricingCatalogSource) (model.PricingCatalogEntry, error) {
				if failure == "fetch" {
					return model.PricingCatalogEntry{}, errors.New("metadata unavailable")
				}
				return model.PricingCatalogEntry{}, nil
			}, func(model.PricingCatalogEntry) error {
				if failure == "fetch" {
					t.Error("failed fetch was published")
				}
				return errors.New("persistence unavailable")
			})
			require.ErrorContains(t, err, "failed for 12 of 12 sources")
			assert.Equal(t, 12, summary.Failed)
			assert.Zero(t, summary.Published)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := refreshPricingCatalogSources(ctx, sources, func(ctx context.Context, source pricingCatalogSource) (model.PricingCatalogEntry, error) {
		return model.PricingCatalogEntry{}, ctx.Err()
	}, func(model.PricingCatalogEntry) error { t.Fatal("cancelled task published"); return nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestConsumerPricingUsesSnapshotAndActualBillingSource(t *testing.T) {
	item := tieredPricing(`tier("base",p*2+c*3)`)
	item.SupportedEndpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
	item.BillingSourcesByGroup = map[string]map[constant.EndpointType][]string{"default": {constant.EndpointTypeOpenAI: {model.BillingSourceConfiguredTariff}}}
	// Consumer uses the already projected snapshot even when the raw expression
	// text is unavailable, and never reinterprets it as an actual-cost tariff.
	delete(item.ConfiguredPrices, item.BillingExpr)
	rows := consumerOperations(item, "default", 1, true)
	require.Len(t, rows, 1)
	assert.Empty(t, rows[0].Prices)
	item.ConfiguredPrices[item.BillingExpr] = billingexpr.CatalogPrices(item.BillingExpr)
	rows = consumerOperations(item, "default", 1, true)
	require.Len(t, rows[0].Prices, 2)
	item.BillingSourcesByGroup["default"][constant.EndpointTypeOpenAI] = []string{model.BillingSourceOpenRouterActualCost}
	rows = consumerOperations(item, "default", 1, true)
	assert.Empty(t, rows[0].Prices)
}

type pricingCatalogTestTransport func(*http.Request) (*http.Response, error)

func (transport pricingCatalogTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestPricingCatalogFetchUsesBoundedMetadataAndConsumerDoesNoHTTP(t *testing.T) {
	client := service.GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	previousTransport := client.Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	var attempts atomic.Int64
	body := `{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text","audio"]},"endpoints":[{"tag":"native","pricing":{"prompt":"0.000001","completion":"0.000002","internal_reasoning":"0.000003","audio_output":"0.0005","web_search":"0.01"}}]}}`
	client.Transport = pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
		attempts.Add(1)
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "https://openrouter.ai/api/v1/models/provider/model/endpoints", request.URL.String())
		assert.Equal(t, "Bearer secret-fixture", request.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	channel := &model.Channel{Id: 990, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Key: "secret-fixture", Models: "provider/model"}
	source := pricingCatalogSource{channel: channel, upstream: "provider/model", endpoint: constant.EndpointTypeOpenAI, fingerprint: model.PricingCatalogSourceFingerprint(channel, "provider/model", constant.EndpointTypeOpenAI)}
	entry, err := fetchPricingCatalogSource(context.Background(), source)
	require.NoError(t, err)
	assert.EqualValues(t, 1, attempts.Load())
	require.Len(t, entry.Offers, 1)
	prices := map[string]model.CatalogPrice{}
	for _, price := range entry.Offers[0].Prices {
		prices[price.Component] = price
	}
	assert.Equal(t, "million_tokens", prices["reasoning_output"].Unit)
	assert.Equal(t, 3.0, *prices["reasoning_output"].AmountUSD)
	assert.Equal(t, "search", prices["web_search"].Unit)
	assert.Equal(t, .01, *prices["web_search"].AmountUSD)
	assert.NotContains(t, prices, "audio_output")
	item := model.Pricing{ModelName: "provider/model", EnableGroup: []string{"default"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": entry.Offers}}
	for range 3 {
		rows := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, item)
		require.Len(t, rows.GroupPrices[0].Operations, 1)
	}
	assert.EqualValues(t, 1, attempts.Load())
	body = strings.Repeat("x", pricingCatalogBodyLimit+1)
	_, err = fetchPricingCatalogSource(context.Background(), source)
	require.ErrorContains(t, err, "body limit")
	started := make(chan struct{})
	client.Transport = pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := refreshPricingCatalogSources(ctx, []pricingCatalogSource{source}, fetchPricingCatalogSource, func(model.PricingCatalogEntry) error { t.Error("cancelled metadata was published"); return nil })
		done <- err
	}()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestPricingCatalogNativeTextRequiresArchitectureProof(t *testing.T) {
	for _, architecture := range []string{`{}`, `{"input_modalities":["text"],"output_modalities":["audio"]}`, `{"input_modalities":["text"],"output_modalities":["image"]}`} {
		body := []byte(`{"data":{"id":"provider/model","architecture":` + architecture + `,"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}}`)
		_, err := normalizeEndpointPricing("provider/model", "text", "output", body)
		require.ErrorContains(t, err, "text-only")
	}
}

func TestPricingCatalogImageFontInputPreservesNativeCoherentPrice(t *testing.T) {
	body := []byte(`{"id":"sourceful/riverflow","endpoints":[{"provider_tag":"sourceful","pricing":[{"billable":"output_image","unit":"image","cost_usd":0.06},{"billable":"input_font","unit":"image","cost_usd":0.03}]}]}`)
	offers, err := normalizeImagePricing("sourceful/riverflow", body)
	require.NoError(t, err)
	selected := model.SelectCatalogOffer(selectPricingCatalogOffers(offers))
	require.NotNil(t, selected)
	require.Len(t, selected.Prices, 2)
	assert.Equal(t, "image_output", selected.Prices[0].Component)
	assert.Equal(t, 0.06, *selected.Prices[0].AmountUSD)
	assert.Equal(t, "font_input", selected.Prices[1].Component)
	assert.Equal(t, "image", selected.Prices[1].Unit)
	assert.Equal(t, 0.03, *selected.Prices[1].AmountUSD)
	item := model.Pricing{CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {*selected}}}
	rows := consumerOperations(item, "default", 1, true)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].Prices, 2)
	for _, price := range rows[0].Prices {
		if price.Component == "font_input" {
			assert.Equal(t, "image", price.Unit)
			require.NotNil(t, price.AmountRub)
			assert.Equal(t, 3.0, *price.AmountRub)
			return
		}
	}
	t.Fatal("font_input native price lost in consumer projection")
}
