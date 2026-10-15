package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
		item.CatalogOffersByGroup = map[string][]model.CatalogOffer{}
		for _, group := range item.EnableGroup {
			item.BillingSourcesByGroup[group] = map[constant.EndpointType][]string{endpoint: {model.BillingSourceConfiguredTariff}}
			offer := model.CatalogOffer{ID: "fixture", Operation: "text", Mode: "sync", MainComponent: "output"}
			if item.QuotaType == 1 {
				offer.Operation, offer.MainComponent = "image_generation", "image_output"
			} else {
				offer.Prices = []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: float(item.ModelRatio * 2)}, {Component: "output", Unit: "million_tokens", AmountUSD: float(item.ModelRatio * item.CompletionRatio * 2)}}
			}
			item.CatalogOffersByGroup[group] = []model.CatalogOffer{offer}
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

func TestEffectivePricingPageSearchIgnoresModelFormatting(t *testing.T) {
	for _, test := range []struct {
		name, modelName, search string
		matches                 bool
	}{
		{"spaces", "openai/gpt-4.1-mini", "  GPT 4.1 MINI  ", true},
		{"compact", "openai/gpt-4.1-mini", "gpt41mini", true},
		{"separators", "openai/gpt-4.1-mini", "OpenAI_GPT_4_1_MINI", true},
		{"display name", "Claude Sonnet 4.5", "claude-sonnet-4-5", true},
		{"unicode dash", "Claude\u2011Sonnet\u20144.5", "claude sonnet 4.5", true},
		{"unicode spaces", "DeepSeek\u00a0R1", "deep seek r1", true},
		{"unicode width", "openai/gpt-4.1-mini", "ＧＰＴ４．１", true},
		{"canonical unicode", "caf\u00e9-model", "cafe\u0301 model", true},
		{"partial name", "google/gemini-2.5-pro-preview", "gemini 2.5 pro", true},
		{"different version", "gpt-4.1-mini", "gpt4.2", false},
		{"different family", "gpt-4.1-mini", "claude", false},
		{"punctuation only", "gpt-4.1-mini", "???", false},
		{"empty", "gpt-4.1-mini", " \t ", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, personal := range []bool{false, true} {
				context := pricingPageFixture()
				context.pricing = context.pricing[:1]
				context.pricing[0].ModelName = test.modelName
				query, err := parseEffectivePricingPageQuery(url.Values{"search": {test.search}}, personal)
				require.NoError(t, err)
				page := buildEffectivePricingPage(context, query, context.project)
				if test.matches {
					assert.Equal(t, 1, page.TotalCount)
					require.Len(t, page.Data, 1)
					assert.Equal(t, test.modelName, page.Data[0].ModelName)
				} else {
					assert.Zero(t, page.TotalCount)
					assert.Empty(t, page.Data)
				}
			}
		})
	}
}

func TestEffectivePricingSmartSearchFiltersBeforePagination(t *testing.T) {
	context := pricingPageFixture()
	context.pricing[0].ModelName = "openai/gpt-4.1-mini"
	context.pricing[2].ModelName = "openai/gpt-4.1"
	context.pricing[2].VendorID = 2
	for _, personal := range []bool{false, true} {
		query, err := parseEffectivePricingPageQuery(url.Values{
			"search": {"GPT 4.1"}, "limit": {"1"}, "offset": {"1"}, "modality": {"TEXT"}, "creator_id": {"2"},
		}, personal)
		require.NoError(t, err)
		page := buildEffectivePricingPage(context, query, context.project)
		assert.Equal(t, 2, page.TotalCount)
		require.Len(t, page.Data, 1)
		assert.Equal(t, "openai/gpt-4.1-mini", page.Data[0].ModelName)
		query.offset = 0
		query.modality = "IMAGE"
		assert.Zero(t, buildEffectivePricingPage(context, query, context.project).TotalCount)
		query.modality = "TEXT"
		creatorID := 1
		query.creatorID = &creatorID
		assert.Zero(t, buildEffectivePricingPage(context, query, context.project).TotalCount)
	}
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

func TestConsumerPricingKeepsKnownPricesOfCheapestOfferAndNativeUnits(t *testing.T) {
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
		require.Len(t, prices, 2)
		assert.InDelta(t, 7*100*.8*multiplier, *prices[0].AmountRub, 1e-9)
		assert.InDelta(t, 3*100*.8*multiplier, *prices[1].AmountRub, 1e-9)
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

func TestConsumerPricingOmitsUnpricedComponentsWithoutChangingSource(t *testing.T) {
	for _, operation := range []string{"transcription", "speech_generation", "text", "image_generation", "text_to_video"} {
		t.Run(operation, func(t *testing.T) {
			prices := []model.CatalogPrice{
				{Component: "input", Unit: "million_tokens", AmountUSD: float(.6)},
				{Component: "audio_output", Unit: "unknown"},
				{Component: "output", Unit: "million_tokens"},
				{Component: "cache_read", Unit: "million_tokens", AmountUSD: float(0)},
				{Component: "image_input", Unit: "unknown", AmountUSD: float(1)},
				{Component: "image_output", Unit: "", AmountUSD: float(1)},
				{Component: "reference_input", Unit: "image", AmountUSD: float(math.NaN())},
				{Component: "additional_reference_input", Unit: "image", AmountUSD: float(math.Inf(1))},
				{Component: "font_input", Unit: "image", AmountUSD: float(-1)},
			}
			item := model.Pricing{CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {{ID: "source", Operation: operation, Mode: "sync", MainComponent: "input", Prices: prices}}}}
			rows := consumerOperations(item, "default", 2, true)
			require.Len(t, rows, 1)
			assert.Equal(t, []consumerUnitPrice{{Component: "input", Unit: "million_tokens", AmountRub: float(120)}, {Component: "cache_read", Unit: "million_tokens", AmountRub: float(0)}}, rows[0].Prices)
			require.Len(t, item.CatalogOffersByGroup["default"][0].Prices, len(prices))
			assert.Nil(t, item.CatalogOffersByGroup["default"][0].Prices[1].AmountUSD)
		})
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
	model.RefreshPricing()
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
	require.Len(t, offers, 3)
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
			if after != nil {
				require.NotEmpty(t, after.PublicAlternatives)
				copied := *after
				copied.PublicAlternatives = nil
				after = &copied
			}
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
	var calls, published, publications, active, maximum atomic.Int64
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
		}, func(entries []model.PricingCatalogEntry) (int, error) {
			publications.Add(1)
			published.Add(int64(len(entries)))
			return len(entries), nil
		})
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
	assert.EqualValues(t, 1, publications.Load(), "all successful metadata must commit together")
	for _, failure := range []string{"fetch", "persist"} {
		t.Run("all_"+failure+"_fail", func(t *testing.T) {
			summary, err := refreshPricingCatalogSources(context.Background(), sources, func(context.Context, pricingCatalogSource) (model.PricingCatalogEntry, error) {
				if failure == "fetch" {
					return model.PricingCatalogEntry{}, errors.New("metadata unavailable")
				}
				return model.PricingCatalogEntry{}, nil
			}, func([]model.PricingCatalogEntry) (int, error) {
				if failure == "fetch" {
					t.Error("failed fetch was published")
				}
				return 0, errors.New("persistence unavailable")
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
	}, func([]model.PricingCatalogEntry) (int, error) { t.Fatal("cancelled task published"); return 0, nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestConsumerPricingUsesProjectedSnapshotOnly(t *testing.T) {
	item := tieredPricing(`tier("base",p*2+c*3)`)
	item.CatalogOffersByGroup = map[string][]model.CatalogOffer{"default": {{ID: "actual", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(7)}}}}}
	rows := consumerOperations(item, "default", 1, true)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].Prices, 1)
	assert.Equal(t, 700.0, *rows[0].Prices[0].AmountRub)
	item.BillingExpr = `tier("changed",p*200+c*300)`
	item.ConfiguredPrices = nil
	assert.Equal(t, rows, consumerOperations(item, "default", 1, true))
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
	body := `{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"native","pricing":{"prompt":"0.000001","completion":"0.000002","internal_reasoning":"0.000003","audio_output":"0.0005","web_search":"0.01"}}]}}`
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
		_, err := refreshPricingCatalogSources(ctx, []pricingCatalogSource{source}, fetchPricingCatalogSource, func([]model.PricingCatalogEntry) (int, error) {
			t.Error("cancelled metadata was published")
			return 0, nil
		})
		done <- err
	}()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestPricingCatalogNativeTextRequiresArchitectureProof(t *testing.T) {
	for _, architecture := range []string{`{"input_modalities":["text"],"output_modalities":["audio"]}`, `{"input_modalities":["text"],"output_modalities":["image"]}`} {
		body := []byte(`{"data":{"id":"provider/model","architecture":` + architecture + `,"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}}`)
		offers, err := normalizeEndpointPricing("provider/model", "text", "output", body)
		require.NoError(t, err)
		assert.Empty(t, offers)
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

func TestConsumerPricingNativeVideoOperationsMatchBoundSourceDescriptors(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
		model.InvalidatePricingCache()
	})
	prices := map[string]float64{}
	require.NoError(t, common.UnmarshalJsonStr(previousPrices, &prices))
	prices["sora-2"] = .3
	encoded, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(encoded)))
	previousRegistry, previousMemoryCache := jsplugin.DefaultRegistry, common.MemoryCacheEnabled
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		jsplugin.DefaultRegistry = previousRegistry
		common.MemoryCacheEnabled = previousMemoryCache
		model.InvalidatePricingCache()
	})
	for _, path := range []string{"../plugins/tasks/openrouter-video/plugin.js", "../plugins/tasks/sora/plugin.js"} {
		source, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = jsplugin.DefaultRegistry.Register(string(source), jsplugin.Options{})
		require.NoError(t, err)
	}
	mapping := `{"aleph-alias":"runway/aleph-2","avatar-alias":"heygen/avatar-iv","edit-alias":"black-forest-labs/flux-video-edit","flux-alias":"black-forest-labs/flux-3-video","seedance-alias":"bytedance/seedance-2.5","upscale-alias":"black-forest-labs/flux-video-upscale"}`
	names := []string{"aleph-alias", "avatar-alias", "edit-alias", "flux-alias", "seedance-alias", "upscale-alias"}
	require.NoError(t, db.Create(&model.Channel{Id: 991, Type: constant.ChannelTypeOpenRouter, Status: 1, Key: "fixture", Models: strings.Join(names, ","), ModelMapping: &mapping}).Error)
	for _, name := range names {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: 991, Enabled: true}).Error)
	}
	require.NoError(t, db.Create(&model.Channel{Id: 992, Type: constant.ChannelTypeOpenAI, Status: 1, Key: "fixture", Models: "sora-2"}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "sora-2", ChannelId: 992, Enabled: true}).Error)
	model.InitChannelCache()
	model.RefreshPricing()
	items := map[string]model.Pricing{}
	for _, item := range model.GetPricing() {
		items[item.ModelName] = item
	}
	for _, name := range append(names, "sora-2") {
		item, exists := items[name]
		require.True(t, exists)
		rows := consumerOperations(item, "default", 1, true)
		operations := map[string]consumerPricingOperation{}
		for _, row := range rows {
			operations[row.Operation] = row
			assert.NotEqual(t, "text", row.Operation, name)
		}
		switch name {
		case "aleph-alias":
			require.Len(t, operations, 1)
			require.Contains(t, operations, "video_editing")
			require.Len(t, operations["video_editing"].Prices, 1)
			assert.Equal(t, "second", operations["video_editing"].Prices[0].Unit)
			assert.InDelta(t, 28.0, *operations["video_editing"].Prices[0].AmountRub, 1e-9)
		case "upscale-alias":
			require.Len(t, operations, 1)
			require.Contains(t, operations, "video_upscaling")
			require.Len(t, operations["video_upscaling"].Prices, 1)
			assert.Equal(t, "megapixel_second", operations["video_upscaling"].Prices[0].Unit)
			assert.InDelta(t, 7.5, *operations["video_upscaling"].Prices[0].AmountRub, 1e-9)
		case "avatar-alias":
			require.Len(t, operations, 1)
			require.Contains(t, operations, "image_to_video")
		case "edit-alias":
			require.Len(t, operations, 1)
			require.Contains(t, operations, "video_editing")
		case "flux-alias", "seedance-alias":
			require.Contains(t, operations, "text_to_video")
			require.Contains(t, operations, "image_to_video")
		case "sora-2":
			require.Len(t, operations, 3)
			require.Contains(t, operations, "remix")
			require.Len(t, operations["remix"].Prices, 1)
			assert.Equal(t, "call", operations["remix"].Prices[0].Unit)
			assert.Equal(t, 30.0, *operations["remix"].Prices[0].AmountRub)
		}
	}
	// A genuinely separate text route sharing an alias remains independently supported.
	require.NoError(t, db.Create(&model.Channel{Id: 993, Type: constant.ChannelTypeOpenAI, Status: 1, Key: "fixture", Models: "edit-alias"}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "edit-alias", ChannelId: 993, Enabled: true}).Error)
	model.InitChannelCache()
	model.RefreshPricing()
	for _, item := range model.GetPricing() {
		if item.ModelName == "edit-alias" {
			rows := consumerOperations(item, "default", 1, true)
			operations := []string{}
			for _, row := range rows {
				operations = append(operations, row.Operation)
			}
			assert.Contains(t, operations, "text")
			assert.Contains(t, operations, "video_editing")
		}
	}
}

func TestPricingCatalogVariantIdentityAndMixedOutputBasis(t *testing.T) {
	for _, suffix := range []string{":batch", ":free"} {
		upstream := "provider/model" + suffix
		body := []byte(`{"data":{"id":"` + upstream + `","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"model_id":"` + upstream + `","pricing":{"prompt":"0.000005","completion":"0.000025"}}]}}`)
		offers, err := normalizeEndpointPricing(upstream, "text", "output", body)
		require.NoError(t, err)
		selected := model.SelectCatalogOffer(offers)
		require.NotNil(t, selected)
		assert.Equal(t, "output", selected.Prices[0].Component)
		assert.Equal(t, 25.0, *selected.Prices[0].AmountUSD)
		mismatched := strings.Replace(string(body), `"model_id":"`+upstream+`"`, `"model_id":"provider/model"`, 1)
		_, err = normalizeEndpointPricing(upstream, "text", "output", []byte(mismatched))
		require.ErrorContains(t, err, "identity")
	}
	for _, output := range []string{"image", "audio"} {
		dimension := output + "_output"
		body := []byte(`{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text","` + output + `"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","` + dimension + `":"0.00003"}}]}}`)
		offers, err := normalizeEndpointPricing("provider/model", "text", "output", body)
		require.NoError(t, err)
		require.Len(t, offers, 1)
		assert.Equal(t, "text", offers[0].Operation)
		if output == "audio" {
			require.NotNil(t, model.SelectCatalogOffer(offers))
			assert.Contains(t, offers[0].Prices, model.CatalogPrice{Component: "audio_output", Unit: "million_tokens", AmountUSD: float(30)})
		} else {
			require.NotNil(t, model.SelectCatalogOffer(offers))
			assert.Contains(t, offers[0].Prices, model.CatalogPrice{Component: "input", Unit: "million_tokens", AmountUSD: float(1)})
			assert.Contains(t, offers[0].Prices, model.CatalogPrice{Component: "output", Unit: "million_tokens", AmountUSD: float(2)})
		}
	}
	body := []byte(`{"data":{"id":"provider/model:free","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[]}}`)
	offers, err := normalizeEndpointPricing("provider/model:free", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 1)
	assert.Nil(t, model.SelectCatalogOffer(offers))
	_, err = normalizeEndpointPricing("provider/model", "text", "output", []byte(`{"data":{"id":"provider/model","architecture":{},"endpoints":[]}}`))
	require.ErrorContains(t, err, "operation")
	for _, fee := range []string{"0", "0.0001"} {
		body := []byte(`{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text","image"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","image_output":"0.00003","unknown_fee":"` + fee + `"}}]}}`)
		offers, err := normalizeEndpointPricing("provider/model", "text", "output", body)
		require.NoError(t, err)
		if fee == "0" {
			require.NotNil(t, model.SelectCatalogOffer(offers))
		} else {
			assert.Nil(t, model.SelectCatalogOffer(offers))
		}
	}

}

func TestPricingCatalogSourceJobsUseNativeCapabilitiesAndExactBatch(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemory
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		model.InvalidatePricingCache()
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.batch_billing_expr": `{"fable:batch":"tier(\"batch\",provider_cost)"}`}))
	mapping := `{"image-alias":"sourceful/riverflow","fable":"anthropic/claude-fable-5.1","fable:batch":"anthropic/claude-fable-5.1:batch","free":"qwen/qwen3.5-9b:free","unsupported":"provider/model:nitro"}`
	require.NoError(t, db.Create(&model.Channel{Id: 991, Type: constant.ChannelTypeOpenRouter, Status: 1, Key: "fixture", Models: "image-alias,fable,fable:batch,free,unsupported", ModelMapping: &mapping}).Error)
	for _, name := range []string{"image-alias", "fable", "fable:batch", "free", "unsupported"} {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: 991, Enabled: true}).Error)
	}
	model.InitChannelCache()
	model.RefreshPricing()
	client := service.GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	previousTransport := client.Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	attempts := 0
	client.Transport = pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
		attempts++
		require.Equal(t, "https://openrouter.ai/api/v1/images/models", request.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"sourceful/riverflow"}]}`)), Header: make(http.Header)}, nil
	})
	sources, err := pricingCatalogSources(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, attempts)
	found := map[string]map[constant.EndpointType]bool{}
	for _, source := range sources {
		if found[source.upstream] == nil {
			found[source.upstream] = map[constant.EndpointType]bool{}
		}
		found[source.upstream][source.endpoint] = true
	}
	assert.True(t, found["sourceful/riverflow"][constant.EndpointTypeImageGeneration])
	assert.Equal(t, map[constant.EndpointType]bool{model.PricingEndpointBatch: true}, found["anthropic/claude-fable-5.1:batch"])
	assert.False(t, found["anthropic/claude-fable-5.1"][model.PricingEndpointBatch])
	assert.NotEmpty(t, found["qwen/qwen3.5-9b:free"])
	assert.NotContains(t, found, "provider/model:nitro")
	client.Transport = pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "https://openrouter.ai/api/v1/models/anthropic/claude-fable-5.1:batch/endpoints", request.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":{"id":"anthropic/claude-fable-5.1:batch","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"model_id":"anthropic/claude-fable-5.1:batch","pricing":{"prompt":"0.000005","completion":"0.000025"}}]}}`)), Header: make(http.Header)}, nil
	})
	for _, source := range sources {
		if source.endpoint != model.PricingEndpointBatch {
			continue
		}
		entry, err := fetchPricingCatalogSource(context.Background(), source)
		require.NoError(t, err)
		require.Len(t, entry.Offers, 1)
		assert.Equal(t, "batch", entry.Offers[0].Mode)
		assert.Equal(t, 25.0, *entry.Offers[0].Prices[0].AmountUSD)
	}
}

func TestEffectivePricingPageFoldsBatchBeforePagingAndKeepsGroupFX(t *testing.T) {
	context := effectivePricingPageContext{
		metadata:     consumerPricingResponse{SchemaVersion: "v2", Currency: "RUB", UserGroup: "default"},
		usableGroups: map[string]string{"default": "Default", "premium": "Premium"}, groupRatios: map[string]float64{"default": 2, "premium": 3}, fxFactor: .5,
		pricing: []model.Pricing{
			{ModelName: "canonical", EnableGroup: []string{"default"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {{ID: "sync", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(10)}}}}}},
			{ModelName: "canonical:batch", EnableGroup: []string{"default", "premium"}, ConsumerCanonicalModelsByGroup: map[string]string{"default": "canonical"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{
				"default": {{ID: "batch-default", Operation: "text", Mode: "batch", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(7)}}}},
				"premium": {{ID: "batch-premium", Operation: "text", Mode: "batch", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(9)}}}},
			}},
		},
	}
	query, err := parseEffectivePricingPageQuery(url.Values{"limit": {"1"}}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Equal(t, 2, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "canonical", page.Data[0].ModelName)
	groups := page.Data[0].GroupPrices
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Operations, 2)
	assert.Equal(t, "batch", groups[0].Operations[0].Mode)
	assert.Equal(t, 700.0, *groups[0].Operations[0].Prices[0].AmountRub)
	query.offset = 1
	page = buildEffectivePricingPage(context, query, context.project)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "canonical:batch", page.Data[0].ModelName)
	require.Len(t, page.Data[0].GroupPrices, 1)
	assert.Equal(t, "premium", page.Data[0].GroupPrices[0].UsingGroup)
	assert.Equal(t, 1350.0, *page.Data[0].GroupPrices[0].Operations[0].Prices[0].AmountRub)
	context.usableGroups = map[string]string{"default": "Default"}
	query.offset = 0
	page = buildEffectivePricingPage(context, query, context.project)
	assert.Equal(t, 1, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "canonical", page.Data[0].ModelName)
	assert.Len(t, context.pricing, 2)
	assert.Equal(t, "canonical:batch", context.pricing[1].ModelName)
}

func TestEffectivePricingPageUsesProvedAliasOperationsForModality(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemory
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		model.InvalidatePricingCache()
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": `{"semantic-index":"tiered_expr"}`, "billing_setting.billing_expr": `{"semantic-index":"tier(\"base\",p*.13)"}`}))
	mapping := `{"semantic-index":"text-embedding-3-large"}`
	require.NoError(t, db.Create(&model.Channel{Id: 993, Type: constant.ChannelTypeOpenAI, Status: 1, Key: "fixture", Models: "semantic-index", ModelMapping: &mapping}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "semantic-index", ChannelId: 993, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Vendor{Id: 11, Name: "Embedding source"}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "semantic-index", VendorID: 11}).Error)
	model.InitChannelCache()
	model.RefreshPricing()
	items := model.GetPricing()
	require.Len(t, items, 1)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, items[0].SupportedEndpointTypes)
	imageOffers, err := normalizeImagePricing("sourceful/riverflow", []byte(`{"id":"sourceful/riverflow","endpoints":[{"provider_tag":"sourceful","pricing":[{"billable":"output_image","unit":"image","cost_usd":0.06}]}]}`))
	require.NoError(t, err)
	textOffers, err := normalizeEndpointPricing("google/gemini-image", "text", "output", []byte(`{"data":{"id":"google/gemini-image","architecture":{"input_modalities":["text"],"output_modalities":["text","image"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","image_output":"0.00003"}}]}}`))
	require.NoError(t, err)
	items = append(items,
		model.Pricing{ModelName: "visual-route", VendorID: 12, EnableGroup: []string{"default"}, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": imageOffers}},
		model.Pricing{ModelName: "mixed-route", VendorID: 12, EnableGroup: []string{"default"}, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": append(append([]model.CatalogOffer{}, imageOffers...), textOffers...)}},
	)
	context := effectivePricingPageContext{metadata: consumerPricingResponse{SchemaVersion: "v2", Currency: "RUB", UserGroup: "default"}, pricing: items, usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1}, fxFactor: 1, vendors: []model.PricingVendor{{ID: 11, Name: "Embedding source"}, {ID: 12, Name: "Image source"}}}
	query, err := parseEffectivePricingPageQuery(url.Values{"modality": {"EMBEDDINGS"}, "limit": {"1"}}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Equal(t, 1, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "semantic-index", page.Data[0].ModelName)
	assert.Equal(t, "EMBEDDINGS", page.Data[0].Modality)
	assert.Equal(t, "embeddings", page.Data[0].GroupPrices[0].Operations[0].Operation)
	assert.Equal(t, []effectivePricingCreator{{ID: 11, Name: "Embedding source"}, {ID: 12, Name: "Image source"}}, page.Creators)
	query.modality = "IMAGE"
	query.limit = 20
	page = buildEffectivePricingPage(context, query, context.project)
	require.Equal(t, 2, page.TotalCount)
	require.Len(t, page.Data, 2)
	assert.Equal(t, "mixed-route", page.Data[0].ModelName)
	assert.Equal(t, "visual-route", page.Data[1].ModelName)
	operations := page.Data[0].GroupPrices[0].Operations
	require.Len(t, operations, 2)
	assert.Equal(t, "image_generation", operations[0].Operation)
	assert.Equal(t, "text", operations[1].Operation)
	query.creatorID = new(int)
	*query.creatorID = 11
	page = buildEffectivePricingPage(context, query, context.project)
	assert.Zero(t, page.TotalCount)
	assert.Empty(t, page.Data)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, items[0].SupportedEndpointTypes)
}

func TestPricingRevisionIncludesCapturedContextAndGuardsBeforePage(t *testing.T) {
	context := pricingPageFixture()
	context.metadata.UserGroup = "default"
	revision, err := consumerPricingRevision("source", context)
	require.NoError(t, err)
	require.NotEmpty(t, revision)
	permuted := context
	permuted.usableGroups = map[string]string{"premium": "Premium", "default": "Default"}
	permuted.groupRatios = map[string]float64{"premium": 2, "default": 1}
	same, err := consumerPricingRevision("source", permuted)
	require.NoError(t, err)
	assert.Equal(t, revision, same)
	for _, change := range []func(*effectivePricingPageContext){
		func(value *effectivePricingPageContext) { value.fxFactor = 2 },
		func(value *effectivePricingPageContext) {
			value.groupRatios = map[string]float64{"default": 3, "premium": 2}
		},
		func(value *effectivePricingPageContext) { value.usableGroups = map[string]string{"default": "Default"} },
		func(value *effectivePricingPageContext) { value.metadata.AutoGroups = []string{"premium"} },
	} {
		changed := context
		change(&changed)
		current, err := consumerPricingRevision("source", changed)
		require.NoError(t, err)
		assert.NotEqual(t, revision, current)
	}
	context.metadata.CatalogRevision = revision
	for _, limit := range []string{"0", "7"} {
		query, err := parseEffectivePricingPageQuery(url.Values{"limit": {limit}, "expected_revision": {revision}}, true)
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		ginContext, _ := gin.CreateTestContext(recorder)
		require.True(t, pricingRevisionMatches(ginContext, query.expectedRevision, context.metadata.CatalogRevision))
		page := buildEffectivePricingPage(context, query, context.project)
		assert.Equal(t, revision, page.CatalogRevision)
		assert.Equal(t, 4, page.TotalCount)
	}
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	assert.False(t, pricingRevisionMatches(ginContext, "stale", revision))
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.JSONEq(t, `{"success":false,"code":"pricing_catalog_revision_mismatch","message":"Pricing catalog revision changed"}`, recorder.Body.String())
	for _, raw := range []string{"expected_revision=", "expected_revision=a&expected_revision=b"} {
		values, err := url.ParseQuery(raw)
		require.NoError(t, err)
		_, err = expectedPricingRevision(values)
		require.Error(t, err)
	}
}

func TestPricingCatalogGeminiKeepsWholeAudioVectorsAndContextOverrides(t *testing.T) {
	body := []byte(`{"data":{"id":"google/gemini-2.5-pro:batch","architecture":{"input_modalities":["text","audio"],"output_modalities":["text"]},"endpoints":[{"model_id":"google/gemini-2.5-pro:batch","tag":"google","pricing":{"prompt":"0.000000625","completion":"0.000005","audio":"0.000000625","input_audio_cache":"0.000000125","input_cache_read":"0.000000125","overrides":[{"min_prompt_tokens":200000,"prompt":"0.00000125","completion":"0.0000075","audio":"0.00000125","input_audio_cache":"0.00000025"}]}}]}}`)
	offers, err := normalizeEndpointPricing("google/gemini-2.5-pro:batch", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 2)
	for index, offer := range offers {
		prices := map[string]float64{}
		for _, price := range offer.Prices {
			require.NotNil(t, price.AmountUSD)
			assert.Equal(t, "million_tokens", price.Unit)
			prices[price.Component] = *price.AmountUSD
		}
		factor := float64(index + 1)
		assert.Equal(t, .625*factor, prices["input"])
		assert.Equal(t, .625*factor, prices["audio_input"])
		assert.Equal(t, .125*factor, prices["audio_cache_read"])
		assert.NotContains(t, prices, "audio_output")
	}
	selected := model.SelectCatalogOffer(offers)
	require.NotNil(t, selected)
	assert.Equal(t, offers[0], *selected)
	item := model.Pricing{CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": offers}}
	rows := consumerOperations(item, "default", 1.2, true)
	require.Len(t, rows, 1)
	audioCache := []consumerUnitPrice{}
	for _, price := range rows[0].Prices {
		if price.Component == "audio_cache_read" {
			audioCache = append(audioCache, price)
		}
	}
	require.Len(t, audioCache, 1)
	assert.Equal(t, 15.0, *audioCache[0].AmountRub)
	zeroBody := strings.Replace(string(body), `"audio":"0.000000625"`, `"audio":"0"`, 1)
	zero, err := normalizeEndpointPricing("google/gemini-2.5-pro:batch", "text", "output", []byte(zeroBody))
	require.NoError(t, err)
	require.NotNil(t, model.SelectCatalogOffer(zero))
	for _, price := range zero[0].Prices {
		if price.Component == "audio_input" {
			assert.Zero(t, *price.AmountUSD)
		}
	}
}

func TestPricingCatalogCalendarOverridesAreReachableWholeVectors(t *testing.T) {
	body := []byte(`{"data":{"id":"provider/calendar","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"calendar","pricing":{"prompt":"0","completion":"0","input_cache_read":"0","overrides":[{"utc_start":0,"utc_end":1400,"prompt":"0.00000132","completion":"0.00000396","input_cache_read":"0.000000132"},{"utc_start":1400,"utc_end":0,"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000066"}]}}]}}`)
	offers, err := normalizeEndpointPricing("provider/calendar", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 2)
	selected := model.SelectCatalogOffer(offers)
	require.NotNil(t, selected)
	prices := map[string]float64{}
	for _, price := range selected.Prices {
		prices[price.Component] = *price.AmountUSD
	}
	require.Len(t, prices, 3)
	for component, expected := range map[string]float64{"input": .66, "output": 1.98, "cache_read": .066} {
		assert.InDelta(t, expected, prices[component], 1e-12)
	}
	overlap := []byte(`{"data":{"id":"provider/calendar","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"calendar","pricing":{"prompt":"0.000009","completion":"0.000004","overrides":[{"utc_days":["saturday","sunday"],"prompt":"0.000007","completion":"0.000002"},{"utc_start":2300,"utc_end":100,"completion":"0.000001"}]}}]}}`)
	offers, err = normalizeEndpointPricing("provider/calendar", "text", "output", overlap)
	require.NoError(t, err)
	pairs := [][2]float64{}
	for _, offer := range offers {
		rates := map[string]float64{}
		for _, price := range offer.Prices {
			rates[price.Component] = *price.AmountUSD
		}
		pairs = append(pairs, [2]float64{rates["input"], rates["output"]})
	}
	assert.ElementsMatch(t, [][2]float64{{9, 1}, {7, 1}, {9, 4}, {7, 2}}, pairs)
	selected = model.SelectCatalogOffer(offers)
	require.NotNil(t, selected)
	assert.Equal(t, 9.0, *selected.Prices[1].AmountUSD)
	assert.Equal(t, 1.0, *selected.Prices[0].AmountUSD)
}

func TestPricingCatalogCalendarValidatesGatesAndContextReachability(t *testing.T) {
	for _, condition := range []string{
		`"utc_start":1260,"utc_end":1400`, `"utc_start":2400,"utc_end":100`,
		`"utc_start":-1,"utc_end":100`, `"utc_start":"14:00","utc_end":100`,
		`"utc_start":0`, `"utc_start":0,"utc_end":0`,
		`"utc_start":null,"utc_end":100`, `"utc_start":0,"utc_end":null`, `"utc_start": null ,"utc_end":100`,
		`"utc_days":[]`, `"utc_days":[0]`, `"utc_days":["Sunday"]`, `"utc_days":["sunday","sunday"]`,
		`"min_prompt_tokens":-1`, `"unrecognized_gate":true`,
		`"min_prompt_tokens":null`, `"min_prompt_tokens": null `,
	} {
		body := []byte(`{"data":{"id":"provider/calendar","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{` + condition + `,"completion":"0"}]}}]}}`)
		_, err := normalizeEndpointPricing("provider/calendar", "text", "output", body)
		require.Error(t, err, condition)
	}
	body := []byte(`{"data":{"id":"provider/calendar","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"context_length":100,"max_prompt_tokens":50,"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":75,"utc_days":["saturday","sunday"],"completion":"0"}]}}]}}`)
	offers, err := normalizeEndpointPricing("provider/calendar", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 1)
	selected := model.SelectCatalogOffer(offers)
	require.NotNil(t, selected)
	assert.Equal(t, 2.0, *selected.Prices[0].AmountUSD)
}

func TestPricingCatalogFailureDiagnosticsAreBoundedAndSanitized(t *testing.T) {
	sources := make([]pricingCatalogSource, pricingCatalogFailureLimit+4)
	for index := range sources {
		sources[index] = pricingCatalogSource{channel: &model.Channel{Id: 43, Key: "super-secret"}, upstream: fmt.Sprintf("public/model-%02d", index), endpoint: constant.EndpointTypeOpenAI}
	}
	summary, err := refreshPricingCatalogSources(context.Background(), sources, func(context.Context, pricingCatalogSource) (model.PricingCatalogEntry, error) {
		return model.PricingCatalogEntry{}, errors.New("https://private.invalid super-secret native endpoint pricing response")
	}, func([]model.PricingCatalogEntry) (int, error) { t.Fatal("failed metadata published"); return 0, nil })
	require.Error(t, err)
	assert.Equal(t, len(sources), summary.Failed)
	assert.True(t, summary.FailuresTruncated)
	require.Len(t, summary.FailedSources, pricingCatalogFailureLimit)
	assert.Equal(t, pricingCatalogFailure{ChannelID: 43, UpstreamModel: "public/model-00", Endpoint: constant.EndpointTypeOpenAI, Class: "metadata_identity_or_schema_invalid"}, summary.FailedSources[0])
	encoded, err := common.Marshal(summary)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "super-secret")
	assert.NotContains(t, string(encoded), "private.invalid")
	summary, err = refreshPricingCatalogSources(context.Background(), sources[:1], func(context.Context, pricingCatalogSource) (model.PricingCatalogEntry, error) {
		return model.PricingCatalogEntry{}, nil
	}, func([]model.PricingCatalogEntry) (int, error) { return 0, errors.New("super-secret DB failure") })
	require.Error(t, err)
	require.Len(t, summary.FailedSources, 1)
	assert.Equal(t, "publication_failed", summary.FailedSources[0].Class)
}

func TestPricingCatalogPromptThresholdRequiresReachableStrictSuccessor(t *testing.T) {
	for _, scenario := range []struct {
		name                       string
		context, prompt, threshold int
		expected                   float64
	}{
		{"equal_context_limit", 100, 0, 100, 2},
		{"equal_prompt_limit", 0, 100, 100, 2},
		{"equal_smaller_limit", 200, 100, 100, 2},
		{"one_below_context_limit", 100, 0, 99, 0},
		{"one_below_prompt_limit", 0, 100, 99, 0},
		{"explicit_zero_reachable", 1, 1, 0, 0},
		{"max_int_unreachable", 0, 0, int(^uint(0) >> 1), 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"data":{"id":"provider/boundary","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"context_length":%d,"max_prompt_tokens":%d,"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":%d,"completion":"0"}]}}]}}`, scenario.context, scenario.prompt, scenario.threshold))
			offers, err := normalizeEndpointPricing("provider/boundary", "text", "output", body)
			require.NoError(t, err)
			selected := model.SelectCatalogOffer(offers)
			require.NotNil(t, selected)
			for _, price := range selected.Prices {
				if price.Component == "output" {
					require.NotNil(t, price.AmountUSD)
					assert.Equal(t, scenario.expected, *price.AmountUSD)
				}
			}
		})
	}
	for _, gate := range []string{`"utc_days":["sunday"]`, `"utc_start":0,"utc_end":1200`} {
		body := []byte(`{"data":{"id":"provider/boundary","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"context_length":1,"max_prompt_tokens":1,"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{` + gate + `,"completion":"0"}]}}]}}`)
		offers, err := normalizeEndpointPricing("provider/boundary", "text", "output", body)
		require.NoError(t, err)
		require.Len(t, offers, 2)
		selected := model.SelectCatalogOffer(offers)
		require.NotNil(t, selected)
		assert.Zero(t, *selected.Prices[0].AmountUSD)
	}
}

func TestConsumerAudioPricingPersonalAndPublicFullAndPage(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ModelCostFXRow{}))
	previousCountToken := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previousCountToken })
	previousQuota := common.QuotaPerUnit
	previousExchange := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuota
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousExchange
	})
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory; model.InvalidatePricingCache() })
	now := time.Now().Unix()
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{Source: "cbr-xml-daily.ru", EffectiveAt: now, FetchedAt: now, Rates: map[string]float64{"USD": 90}}, 0))
	require.NoError(t, db.Create(&model.User{Id: 2198, Username: "audio-pricing-user", Password: "fixture", Group: "default", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 998, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-realtime-mini,gpt-4o-transcribe-diarize,gpt-4o-mini-tts,gpt-transcribe"}).Error)
	for _, name := range []string{"gpt-realtime-mini", "gpt-4o-transcribe-diarize", "gpt-4o-mini-tts", "gpt-transcribe"} {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: 998, Enabled: true}).Error)
	}
	withTieredBillingConfig(t, map[string]string{"gpt-realtime-mini": "tiered_expr", "gpt-4o-transcribe-diarize": "tiered_expr", "gpt-4o-mini-tts": "tiered_expr", "gpt-transcribe": "tiered_expr"}, map[string]string{"gpt-realtime-mini": `tier("base",p*.6+c*2.4+cr*.06+ai*10+ao*20+img*.8)`, "gpt-4o-transcribe-diarize": `tier("base",p*2.5+c*10)`, "gpt-4o-mini-tts": `tier("base",p*.6+c*12+ao*12)`, "gpt-transcribe": `tier("base",p*4.5)`})
	model.InitChannelCache()
	model.RefreshPricing()
	var expected []consumerPricingModel
	for _, handler := range []gin.HandlerFunc{GetEffectivePricing, GetEffectivePricingByGroup} {
		for _, query := range []string{"", "?limit=100"} {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/pricing"+query, nil)
			ctx.Set("id", 2198)
			handler(ctx)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var envelope struct {
				Data struct {
					Data []consumerPricingModel `json:"data"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Len(t, envelope.Data.Data, 4)
			if expected == nil {
				expected = envelope.Data.Data
			} else {
				assert.ElementsMatch(t, expected, envelope.Data.Data)
			}
			for _, item := range envelope.Data.Data {
				require.Len(t, item.GroupPrices, 1)
				require.Len(t, item.GroupPrices[0].Operations, 1)
				operation := item.GroupPrices[0].Operations[0]
				prices := map[string]consumerUnitPrice{}
				for _, price := range operation.Prices {
					prices[price.Component] = price
				}
				if item.ModelName == "gpt-realtime-mini" {
					assert.Equal(t, "realtime", operation.Operation)
					require.Len(t, prices, 6)
					for _, price := range prices {
						assert.Equal(t, "million_tokens", price.Unit)
						require.NotNil(t, price.AmountRub)
					}
				} else if item.ModelName == "gpt-4o-mini-tts" {
					assert.Equal(t, "speech_generation", operation.Operation)
					require.Len(t, prices, 1)
					assert.Equal(t, "million_tokens", prices["input"].Unit)
					require.NotNil(t, prices["input"].AmountRub)
					assert.InDelta(t, 54.0, *prices["input"].AmountRub, 1e-9)
				} else {
					assert.Equal(t, "transcription", operation.Operation)
					assert.Equal(t, "million_tokens", prices["input"].Unit)
					require.NotNil(t, prices["input"].AmountRub)
					if item.ModelName == "gpt-transcribe" {
						require.Len(t, prices, 1)
						assert.InDelta(t, 405.0, *prices["input"].AmountRub, 1e-9)
					} else {
						require.Len(t, prices, 2)
						assert.Equal(t, "million_tokens", prices["output"].Unit)
						require.NotNil(t, prices["output"].AmountRub)
					}
				}
			}
		}
	}
}

func TestPublicPricingModelsTagsPagingAndSafeTariffs(t *testing.T) {
	ctx := pricingPageFixture()
	ctx.groupRatios["default"] = 3
	for i := range ctx.pricing {
		ctx.pricing[i].Tags = "TOP, text;Featured| audio"
	}
	ctx.pricing[1].Tags = "top image"
	ctx.pricing = append(ctx.pricing, model.Pricing{ModelName: "not-top", Tags: "stopping,text", EnableGroup: []string{"default"}})
	before, err := common.Marshal(ctx.pricing)
	require.NoError(t, err)
	query, err := parsePublicPricingModelsQuery("tag=%20TOP%20&tag=TEXT&page_size=1")
	require.NoError(t, err)
	page := buildPublicPricingModelsPage(ctx, query)
	assert.Equal(t, 2, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "a-text", page.Data[0].ModelName)
	require.Len(t, page.Data[0].Tariff.Operations, 1)
	operation := page.Data[0].Tariff.Operations[0]
	assert.Equal(t, 600., *operation.Prices[0].AmountRub)
	query.page = 2
	page = buildPublicPricingModelsPage(ctx, query)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "z-text", page.Data[0].ModelName)
	query.page = 9007199254740991
	assert.Empty(t, buildPublicPricingModelsPage(ctx, query).Data)
	query.tags = []string{"unknown"}
	assert.Zero(t, buildPublicPricingModelsPage(ctx, query).TotalCount)
	after, err := common.Marshal(ctx.pricing)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
	bytes, err := common.Marshal(page)
	require.NoError(t, err)
	for _, forbidden := range []string{"amount_usd", "ratio", "formula", "accounting", "billing_sources", "expression", "group_prices"} {
		assert.NotContains(t, string(bytes), `"`+forbidden+`"`)
	}
	query, err = parsePublicPricingModelsQuery("tag=top")
	require.NoError(t, err)
	assert.Equal(t, 10, query.pageSize)
	assert.Equal(t, 1, query.page)
	ctx.pricing = nil
	for i := range 12 {
		ctx.pricing = append(ctx.pricing, model.Pricing{ModelName: fmt.Sprintf("model-%02d", i), Tags: "top", EnableGroup: []string{"default"}})
	}
	page = buildPublicPricingModelsPage(ctx, query)
	assert.Len(t, page.Data, 10)
	assert.Equal(t, 12, page.TotalCount)
}

func TestPublicPricingModelsInvalidQuery(t *testing.T) {
	for _, raw := range []string{"", "tag=", "tag=top,text", "tag=top%20text", "tag=top;", "tag=%zz", "tag=top&p=0", "tag=top&p=-1", "tag=top&p=1.1", "tag=top&p=+1", "tag=top&p=1&p=2", "tag=top&page_size=0", "tag=top&page_size=101", "tag=top&page_size=1&page_size=1", "tag=top&group=default", "tag=top&using_group=premium", "tag=top&limit=10", "tag=top&p=9007199254740992"} {
		t.Run(raw, func(t *testing.T) { _, err := parsePublicPricingModelsQuery(raw); require.Error(t, err) })
	}
}

func TestPublicPricingModelsConditionalAlternativesAndUnknown(t *testing.T) {
	ctx := pricingPageFixture()
	ctx.groupRatios["default"] = 2
	ctx.fxFactor = .9
	offers := []model.CatalogOffer{
		{ID: "conditional:a", Operation: "speech_generation", Mode: "sync", MainComponent: "input", PublicConditional: true, PublicConditions: []billingexpr.TierCondition{{Var: "len", Op: "<=", Value: 100}}, Prices: []model.CatalogPrice{{Component: "input", Unit: "million_characters", AmountUSD: float(0)}}},
		{ID: "conditional:b", Operation: "speech_generation", Mode: "sync", MainComponent: "input", PublicConditional: true, PublicConditions: []billingexpr.TierCondition{{Var: "len", Op: "<=", Value: 100, Negated: true}}, Prices: []model.CatalogPrice{{Component: "input", Unit: "million_characters", AmountUSD: float(5)}}},
		{ID: "unknown", Operation: "transcription", Mode: "sync", MainComponent: "audio_input", Prices: []model.CatalogPrice{{Component: "audio_input", Unit: "minute"}}},
	}
	item := model.Pricing{PublicCatalogOffersByGroup: map[string][]model.CatalogOffer{"default": offers}}
	result := buildPublicModelTariff(ctx, item)
	require.Len(t, result.Operations, 2)
	speech := result.Operations[0]
	assert.True(t, speech.IsFrom)
	assert.False(t, speech.IsFree)
	require.Len(t, speech.Tiers, 2)
	assert.Equal(t, 0., *speech.Tiers[0].Prices[0].AmountRub)
	assert.Equal(t, 900., *speech.Tiers[1].Prices[0].AmountRub)
	assert.Equal(t, "million_characters", speech.Tiers[1].Prices[0].Unit)
	assert.Equal(t, "not", speech.Tiers[1].Condition.Kind)
	unknown := result.Operations[1]
	assert.False(t, unknown.IsFree)
	require.Len(t, unknown.Tiers, 1)
	assert.Nil(t, unknown.Tiers[0].Prices[0].AmountRub)
	guard := publicPricingGuard(billingexpr.TierCondition{Op: "||", Operands: []billingexpr.TierCondition{{Var: "hour", Arg: "UTC", Op: "<", Value: 4}, {Op: "&&", Operands: []billingexpr.TierCondition{{Var: "p", Op: ">", Value: 3}, {Var: "c", Op: "==", Value: 0}}}}, Negated: true})
	assert.Equal(t, "not", guard.Kind)
	require.Len(t, guard.Conditions, 1)
	assert.Equal(t, "any_of", guard.Conditions[0].Kind)
	assert.Equal(t, "all_of", guard.Conditions[0].Conditions[1].Kind)
}

func TestPublicNativePricingGuardsRetainCalendarAndThreshold(t *testing.T) {
	body := []byte(`{"data":{"id":"provider/guarded","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":100,"utc_start":2200,"utc_end":200,"utc_days":["monday"],"completion":"0"}]}}]}}`)
	offers, err := normalizeEndpointPricing("provider/guarded", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 2)
	for _, offer := range offers {
		require.NotEmpty(t, offer.PublicConditions)
		assert.True(t, offer.PublicConditional)
	}
	encoded, err := common.Marshal(offers)
	require.NoError(t, err)
	var restored []model.CatalogOffer
	require.NoError(t, common.Unmarshal(encoded, &restored))
	assert.Equal(t, offers, restored)
	ctx := pricingPageFixture()
	item := model.Pricing{PublicCatalogOffersByGroup: map[string][]model.CatalogOffer{"default": restored}}
	tariff := buildPublicModelTariff(ctx, item)
	require.Len(t, tariff.Operations, 1)
	require.Len(t, tariff.Operations[0].Tiers, 2)
	wire, err := common.Marshal(tariff)
	require.NoError(t, err)
	assert.Contains(t, string(wire), "utc_minute")
	assert.Contains(t, string(wire), "weekday")
	assert.Contains(t, string(wire), "any_of")
	assert.Contains(t, string(wire), "input")
	assert.NotContains(t, string(wire), "amount_usd")
	assert.NotContains(t, string(wire), "native:")
}

func TestPublicNativeFreshCatalogThroughFetchAndSnapshot(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	previousCatalog := previousOptions[model.PricingCatalogOptionKey]
	if previousCatalog == "" {
		previousCatalog = `{"version":1,"entries":{}}`
	}
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, model.UpdateOption(model.PricingCatalogOptionKey, previousCatalog))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.ModelCostFXRow{}))
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory; model.InvalidatePricingCache() })
	previousQuota := common.QuotaPerUnit
	previousExchange := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuota
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousExchange
	})
	now := time.Now().Unix()
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{Source: "cbr-xml-daily.ru", EffectiveAt: now, FetchedAt: now, Rates: map[string]float64{"USD": 90}}, 0))
	channel := &model.Channel{Id: 990, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Key: "secret-fixture", Models: "provider/model", Group: "default"}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "provider/model", ChannelId: channel.Id, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "provider/model", Tags: "top text", Status: 1, NameRule: model.NameRuleExact}).Error)
	client := service.GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	oldTransport := client.Transport
	t.Cleanup(func() { client.Transport = oldTransport })
	attempts := 0
	client.Transport = pricingCatalogTestTransport(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"native","pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":100,"completion":"0"}]}}]}}`))}, nil
	})
	entry, err := fetchPricingCatalogSource(context.Background(), pricingCatalogSource{channel: channel, upstream: "provider/model", endpoint: constant.EndpointTypeOpenAI, fingerprint: model.PricingCatalogSourceFingerprint(channel, "provider/model", constant.EndpointTypeOpenAI)})
	require.NoError(t, err)
	require.Len(t, entry.Offers, 1)
	require.Len(t, entry.Offers[0].PublicAlternatives, 2)
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	model.InitChannelCache()
	model.RefreshPricing()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/effective/models?tag=top&tag=text", nil)
	GetPublicPricingModels(ctx)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
	var wire struct {
		Data publicPricingModelsPage `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &wire))
	require.Len(t, wire.Data.Data, 1)
	operations := wire.Data.Data[0].Tariff.Operations
	require.Len(t, operations, 1)
	require.Len(t, operations[0].Tiers, 2)
	assert.True(t, operations[0].IsFrom)
	assert.False(t, operations[0].IsFree)
	for _, tier := range operations[0].Tiers {
		require.NotEmpty(t, tier.Prices)
		require.NotNil(t, tier.Condition)
		for _, price := range tier.Prices {
			require.NotNil(t, price.AmountRub)
		}
	}
	assert.Equal(t, 1, attempts)
	require.Len(t, entry.Offers[0].PublicAlternatives, 2)
	assert.Equal(t, "native:0:0", entry.Offers[0].PublicAlternatives[0].ID)
	revision := wire.Data.CatalogRevision
	require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "provider/model").Update("tags", "top video").Error)
	model.RefreshPricing()
	for _, category := range []string{"text", "video"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/effective/models?tag=top&tag="+category, nil)
		GetPublicPricingModels(ctx)
		require.Equal(t, 200, recorder.Code, recorder.Body.String())
		var updated struct {
			Data publicPricingModelsPage `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &updated))
		assert.NotEqual(t, revision, updated.Data.CatalogRevision)
		if category == "text" {
			assert.Empty(t, updated.Data.Data)
		} else {
			require.Len(t, updated.Data.Data, 1)
			assert.Equal(t, []string{"top", "video"}, updated.Data.Data[0].Tags)
		}
	}
	assert.Equal(t, []string{"text", "top"}, wire.Data.Data[0].Tags)
	assert.Equal(t, 1, attempts)
	imageChannel := &model.Channel{Id: 992, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Key: "fixture", Models: "google/gemini-image", Group: "default"}
	require.NoError(t, db.Create(imageChannel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "google/gemini-image", ChannelId: 992, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "google/gemini-image", Tags: "top image", Status: 1, NameRule: model.NameRuleExact}).Error)
	textOffers, err := normalizeEndpointPricing("google/gemini-image", "text", "output", []byte(`{"data":{"id":"google/gemini-image","architecture":{"input_modalities":["text","image"],"output_modalities":["text","image"]},"endpoints":[{"tag":"studio","pricing":{"prompt":"0.0000005","completion":"0.000003","image_output":"0.00006"}}]}}`))
	require.NoError(t, err)
	imageOffers, err := normalizeImagePricing("google/gemini-image", []byte(`{"id":"google/gemini-image","endpoints":[{"provider_tag":"studio","pricing":[{"billable":"output_image","unit":"token","cost_usd":0.00006}]}]}`))
	require.NoError(t, err)
	for endpoint, offers := range map[constant.EndpointType][]model.CatalogOffer{constant.EndpointTypeOpenAI: textOffers, constant.EndpointTypeImageGeneration: imageOffers} {
		require.NoError(t, model.PublishPricingCatalogEntry(model.PricingCatalogEntry{ChannelID: 992, UpstreamModel: "google/gemini-image", Endpoint: endpoint, Source: "https://openrouter.ai/api/v1/models/google/gemini-image/endpoints", FetchedAt: time.Now().UTC().Format(time.RFC3339Nano), Fingerprint: model.PricingCatalogSourceFingerprint(imageChannel, "google/gemini-image", endpoint), Offers: selectPricingCatalogOffers(offers)}))
	}
	model.InitChannelCache()
	model.RefreshPricing()
	imageRecorder := httptest.NewRecorder()
	imageContext, _ := gin.CreateTestContext(imageRecorder)
	imageContext.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/effective/models?tag=top&tag=image", nil)
	GetPublicPricingModels(imageContext)
	require.Equal(t, http.StatusOK, imageRecorder.Code, imageRecorder.Body.String())
	require.NoError(t, common.Unmarshal(imageRecorder.Body.Bytes(), &wire))
	require.Len(t, wire.Data.Data, 1)
	var imagePrices []consumerUnitPrice
	for _, operation := range wire.Data.Data[0].Tariff.Operations {
		if operation.Operation == "image_generation" {
			imagePrices = operation.Prices
		}
	}
	require.Len(t, imagePrices, 2)
	for _, price := range imagePrices {
		require.NotNil(t, price.AmountRub)
		assert.Equal(t, "million_tokens", price.Unit)
		if price.Component == "input" {
			assert.InDelta(t, 45., *price.AmountRub, 1e-9)
		} else {
			assert.Equal(t, "image_output", price.Component)
			assert.InDelta(t, 5400., *price.AmountRub, 1e-9)
		}
	}

}

func TestEffectivePricingOverviewMatchesNativePagesFromCapturedContext(t *testing.T) {
	context := effectivePricingPageContext{
		metadata:     consumerPricingResponse{SchemaVersion: "v2", CatalogRevision: "captured-A", Currency: "RUB", UserGroup: "default", AutoGroups: []string{}},
		usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1}, fxFactor: 1,
		vendors: []model.PricingVendor{{ID: 7, Name: "Creator"}},
	}
	for _, modality := range []string{"TEXT", "IMAGE", "VIDEO", "EMBEDDINGS", "RERANK", "OTHER"} {
		operation := map[string]string{"TEXT": "text", "IMAGE": "image_generation", "VIDEO": "text_to_video", "EMBEDDINGS": "embeddings", "RERANK": "rerank", "OTHER": "transcription"}[modality]
		for _, name := range []string{"b", "A", "a", "unknown"} {
			var amount *float64
			if name != "unknown" {
				amount = float(0)
			}
			context.pricing = append(context.pricing, model.Pricing{ModelName: modality + "/" + name, VendorID: 7, EnableGroup: []string{"default"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {{ID: name, Operation: operation, Mode: "sync", MainComponent: "input", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: amount}}}}}})
		}
	}
	for _, search := range []string{"", "/a", "unknown"} {
		query := effectivePricingPageQuery{limit: 2, search: search, orderBy: "NAME", direction: "asc"}
		overview := buildEffectivePricingOverview(context, query)
		require.Len(t, overview.Sections, 6)
		assert.Equal(t, "captured-A", overview.CatalogRevision)
		for _, section := range overview.Sections {
			query.modality = section.Modality
			page := buildEffectivePricingPage(context, query, context.project)
			assert.Equal(t, page.TotalCount, section.TotalCount)
			assert.Equal(t, page.Data, section.Data)
			assert.LessOrEqual(t, len(section.Data), 2)
		}
	}
	// A new captured context cannot affect a previously constructed overview.
	first := buildEffectivePricingOverview(context, effectivePricingPageQuery{limit: 1, orderBy: "NAME", direction: "asc"})
	context.metadata.CatalogRevision = "captured-B"
	context.groupRatios = map[string]float64{"default": 9}
	second := buildEffectivePricingOverview(context, effectivePricingPageQuery{limit: 1, orderBy: "NAME", direction: "asc"})
	assert.Equal(t, "captured-A", first.CatalogRevision)
	assert.Equal(t, "captured-B", second.CatalogRevision)
}

func TestEffectivePricingOverviewUsesOneRevisionDuringControlledRebuild(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ModelCostFXRow{}))
	previousQuota, previousExchange := common.QuotaPerUnit, operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuota
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousExchange
	})

	now := time.Now().Unix()
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{Source: "cbr-xml-daily.ru", EffectiveAt: now, FetchedAt: now, Rates: map[string]float64{"USD": 90}}, 0))
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory; model.InvalidatePricingCache() })
	names := []string{"chat-fixture", "gpt-image-1", "sora-2", "text-embedding-3-small", "rerank-fixture", "whisper-1"}
	require.NoError(t, db.Create(&model.Channel{Id: 999, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: strings.Join(names, ","), Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1000, Type: constant.ChannelTypeJina, Status: common.ChannelStatusEnabled, Models: "rerank-fixture", Group: "default"}).Error)
	for index, name := range names {
		channelID := 999
		if index == 4 {
			channelID = 1000
		}
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: channelID, Enabled: true}).Error)
		endpoints := []string{"openai", "image-generation", "openai-video", "embeddings", string(constant.EndpointTypeJinaRerank), "audio-transcription"}
		require.NoError(t, db.Create(&model.Model{ModelName: name, Description: "captured-old", Endpoints: fmt.Sprintf(`{"%s":"/fixture"}`, endpoints[index])}).Error)
	}
	model.InitChannelCache()
	model.RefreshPricing()
	require.NoError(t, db.Model(&model.Model{}).Where("model_name IN ?", names).Update("description", "captured-new").Error)
	entered, release := make(chan struct{}), make(chan struct{})
	releaseBuild := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseBuild)
	var once sync.Once
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:overview_build_gate", func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	t.Cleanup(func() { db.Callback().Row().Remove("test:overview_build_gate") })
	model.InvalidatePricingCache()

	var httpAttempts atomic.Int64
	router := gin.New()
	router.GET("/api/pricing/effective/overview", func(c *gin.Context) { httpAttempts.Add(1); GetEffectivePricingOverview(c) })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = time.Second
	type overviewResult struct {
		Search  string
		Data    effectivePricingOverviewData
		Elapsed time.Duration
		Err     error
	}
	invoke := func(search string) overviewResult {
		result := overviewResult{Search: search}
		started := time.Now()
		response, err := client.Get(server.URL + "/api/pricing/effective/overview?limit=1&search=" + url.QueryEscape(search))
		if err != nil {
			result.Err = err
			return result
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			result.Err = err
			return result
		}
		if response.StatusCode != http.StatusOK {
			result.Err = fmt.Errorf("overview HTTP %d: %s", response.StatusCode, body)
			return result
		}
		var wire struct {
			Data effectivePricingOverviewData `json:"data"`
		}
		result.Err = common.Unmarshal(body, &wire)
		result.Data = wire.Data
		result.Elapsed = time.Since(started)
		return result
	}
	answers := make(chan overviewResult, 2)
	for _, search := range []string{"", "fixture"} {
		go func() { answers <- invoke(search) }()
	}
	var first, searchResult overviewResult
	for range 2 {
		answer := <-answers
		require.NoError(t, answer.Err)
		if answer.Search == "" {
			first = answer
		} else {
			searchResult = answer
		}
	}
	assert.EqualValues(t, 2, httpAttempts.Load())
	assert.Equal(t, first.Data.CatalogRevision, searchResult.Data.CatalogRevision)
	for _, section := range searchResult.Data.Sections {
		assert.LessOrEqual(t, len(section.Data), 1)
		for _, item := range section.Data {
			assert.Equal(t, "captured-old", item.Description)
		}
	}
	t.Logf("parallel overview/search: HTTP attempts=%d, overview=%s, search=%s, one revision=%s", httpAttempts.Load(), first.Elapsed, searchResult.Elapsed, first.Data.CatalogRevision)
	select {
	case <-entered:
	case <-time.After(time.Second):
		releaseBuild()
		t.Fatal("background rebuild did not reach barrier")
	}
	releaseBuild()
	model.RefreshPricing()
	second := invoke("")
	require.NoError(t, second.Err)
	require.Len(t, first.Data.Sections, 6)
	require.Len(t, second.Data.Sections, 6)
	assert.NotEqual(t, first.Data.CatalogRevision, second.Data.CatalogRevision)
	for index, section := range first.Data.Sections {
		require.Len(t, section.Data, 1, section.Modality)
		assert.Equal(t, "captured-old", section.Data[0].Description)
		require.Len(t, second.Data.Sections[index].Data, 1)
		assert.Equal(t, "captured-new", second.Data.Sections[index].Data[0].Description)
	}
}

func TestLegacyPricingCapturesEndpointMapWithModels(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory; model.InvalidatePricingCache() })
	require.NoError(t, db.Create(&model.User{Id: 2200, Username: "snapshot-endpoint-user", Password: "fixture", Group: "default", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1001, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "snapshot-endpoint-model", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "snapshot-endpoint-model", ChannelId: 1001, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "snapshot-endpoint-model", Description: "captured-old", Endpoints: `{"fixture-old":"/old-path"}`}).Error)
	model.InitChannelCache()
	model.RefreshPricing()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:legacy_pricing_capture", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	t.Cleanup(func() { db.Callback().Query().Remove("test:legacy_pricing_capture") })
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
	ctx.Set("id", 2200)
	done := make(chan struct{})
	go func() { GetPricing(ctx); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("request did not reach captured-snapshot barrier")
	}
	require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "snapshot-endpoint-model").Updates(map[string]any{"description": "captured-new", "endpoints": `{"fixture-new":"/new-path"}`}).Error)
	model.InvalidatePricingCache()
	model.RefreshPricing()
	close(release)
	<-done
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var wire struct {
		Data      []model.Pricing                `json:"data"`
		Endpoints map[string]common.EndpointInfo `json:"supported_endpoint"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &wire))
	require.Len(t, wire.Data, 1)
	assert.Equal(t, "captured-old", wire.Data[0].Description)
	assert.Contains(t, wire.Data[0].SupportedEndpointTypes, constant.EndpointType("fixture-old"))
	assert.Equal(t, "/old-path", wire.Endpoints["fixture-old"].Path)
	assert.NotContains(t, wire.Endpoints, "fixture-new")
	current, _, _, err := model.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	require.Len(t, current, 1)
	assert.Equal(t, "captured-new", current[0].Description)
}
