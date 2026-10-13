package controller

import (
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEffectiveOperationTariffsSoraActionsRespectBillingScope(t *testing.T) {
	basis := map[string]jsplugin.LegacyPriceBasis{
		constant.TaskActionTextToVideo:  {Unit: "second", UsageField: "seconds"},
		constant.TaskActionImageToVideo: {Unit: "second", UsageField: "seconds"},
		constant.TaskActionRemix:        {Unit: "call"},
	}
	item := model.Pricing{
		ModelName: "sora-2", QuotaType: 1, ModelPrice: 0.3,
		LegacyVideoPriceBasisByGroup: map[string][]map[string]jsplugin.LegacyPriceBasis{"default": {basis}},
	}
	group := buildEffectiveGroupPricing(item, "default", 1.45, 1.21677185)
	tariff := buildOperationTariff(item, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
	require.Equal(t, "priced", tariff.Status)
	assert.Equal(t, "task", tariff.BillingSurface)
	require.Len(t, tariff.Tiers, 3)
	for index, action := range []string{constant.TaskActionTextToVideo, constant.TaskActionImageToVideo, constant.TaskActionRemix} {
		tier := tariff.Tiers[index]
		require.NotNil(t, tier.Condition)
		assert.Equal(t, "task_action", tier.Condition.Kind)
		require.Len(t, tier.Condition.Guards, 1)
		assert.Equal(t, map[string]string{"action": action}, tier.Condition.Guards[0].Values)
		assert.False(t, tier.Condition.Guards[0].Negated)
		assert.Equal(t, basis[action].Unit, tier.UnitPrices[0].Unit)
		assert.InDelta(t, 36.5031555, tier.UnitPrices[0].AmountRub, 1e-9)
	}
	assert.Equal(t, "call", group.Tiers[0].UnitPrices[0].Unit)
	encoded, err := common.Marshal(item)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "LegacyVideoPriceBasis")

	t.Run("pro price", func(t *testing.T) {
		pro := item
		pro.ModelName, pro.ModelPrice = "sora-2-pro", 0.5
		actual := buildOperationTariff(pro, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
		require.Equal(t, "priced", actual.Status)
		assert.InDelta(t, 60.8385925, actual.Tiers[0].UnitPrices[0].AmountRub, 1e-9)
	})

	t.Run("zero", func(t *testing.T) {
		free := item
		free.ModelPrice = 0
		actual := buildOperationTariff(free, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
		require.Equal(t, "priced", actual.Status)
		assert.Zero(t, actual.Tiers[0].UnitPrices[0].AmountRub)
	})
	t.Run("group and mixed routes", func(t *testing.T) {
		mixed := item
		mixed.LegacyVideoPriceBasisByGroup = maps.Clone(item.LegacyVideoPriceBasisByGroup)
		mixed.LegacyVideoPriceBasisByGroup["all"] = []map[string]jsplugin.LegacyPriceBasis{nil}
		actual := buildOperationTariff(mixed, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
		assert.Equal(t, "request_dependent", actual.Status)
		assert.Empty(t, actual.Tiers)
		assert.Equal(t, []string{"legacy_price_basis_unavailable"}, actual.Limitations)
		mixed.LegacyVideoPriceBasisByGroup["all"] = []map[string]jsplugin.LegacyPriceBasis{basis}
		actual = buildOperationTariff(mixed, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
		assert.Equal(t, "priced", actual.Status)
		other := group
		other.UsingGroup = "premium"
		actual = buildOperationTariff(item, other, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
		assert.Equal(t, "request_dependent", actual.Status)
		conflicting := maps.Clone(basis)
		conflicting[constant.TaskActionTextToVideo] = jsplugin.LegacyPriceBasis{Unit: "call"}
		mixed.LegacyVideoPriceBasisByGroup["all"] = []map[string]jsplugin.LegacyPriceBasis{conflicting}
		assert.Equal(t, "request_dependent", buildOperationTariff(mixed, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff).Status)
	})
	t.Run("per call exemption", func(t *testing.T) {
		previous := constant.TaskPricePatches
		constant.TaskPricePatches = []string{item.ModelName}
		t.Cleanup(func() { constant.TaskPricePatches = previous })
		actual := buildOperationTariff(item, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
		require.Len(t, actual.Tiers, 3)
		for _, tier := range actual.Tiers {
			assert.Equal(t, "call", tier.UnitPrices[0].Unit)
		}
		unknown := item
		unknown.LegacyVideoPriceBasisByGroup = nil
		actual = buildOperationTariff(unknown, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
		assert.Equal(t, "priced", actual.Status)
		assert.Equal(t, "call", actual.Tiers[0].UnitPrices[0].Unit)
	})
	t.Run("expressions and other endpoints", func(t *testing.T) {
		actual := buildOperationTariff(item, group, constant.EndpointTypeOpenAI, model.BillingSourceConfiguredTariff)
		assert.Equal(t, group.Tiers, actual.Tiers)
		actual = buildOperationTariff(item, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceOpenRouterActualCost)
		assert.Equal(t, "request_dependent", actual.Status)
		assert.Equal(t, []string{"missing_display_expression"}, actual.Limitations)
		expressionItem := item
		expressionItem.BillingMode = billing_setting.BillingModeTieredExpr
		expressionItem.BillingExpr = `tier("rate", u("seconds") * 0.9)`
		expressionItem.BillingUsageSchema = map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "number", Unit: "second"}}
		actual = buildOperationTariff(expressionItem, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
		require.Equal(t, "priced", actual.Status)
		require.Len(t, actual.Tiers, 1)
		assert.Nil(t, actual.Tiers[0].Condition)
		assert.InDelta(t, 109.5094665, actual.Tiers[0].UnitPrices[0].AmountRub, 1e-9)
	})
}

func TestEffectiveOperationTariffsNumericUsageConditions(t *testing.T) {
	expression := `tier("base", (u("requested_seconds") > 0 ? u("requested_seconds") : 10) * 0.1)`
	item := tieredPricing(expression)
	item.BillingUsageSchema = map[string]jsplugin.UsageFieldSchema{"requested_seconds": {Type: "number", Unit: "second"}}
	item.IndicativeBillingExprByEndpoint = map[constant.EndpointType]string{constant.EndpointTypeOpenAIVideo: expression}
	tariff := buildOperationTariff(item, effectiveGroupPricing{EffectiveBillingRatio: 1.5}, constant.EndpointTypeOpenAIVideo, model.BillingSourceOpenRouterActualCost)
	require.Equal(t, "priced", tariff.Status)
	assert.Equal(t, model.BillingSourceOpenRouterActualCost, tariff.Source)
	require.Len(t, tariff.Tiers, 2)
	for index, tier := range tariff.Tiers {
		require.NotNil(t, tier.Condition)
		assert.Equal(t, "usage_conditions", tier.Condition.Kind)
		require.Len(t, tier.Condition.Guards, 1)
		guard := tier.Condition.Guards[0]
		assert.NotNil(t, guard.Values)
		assert.Empty(t, guard.Values)
		assert.Equal(t, index == 1, guard.Negated)
		require.NotNil(t, guard.Numeric)
		assert.Equal(t, "requested_seconds", guard.Numeric.Field)
		assert.Equal(t, "gt", guard.Numeric.Operator)
		assert.Equal(t, "second", guard.Numeric.Unit)
		assert.Zero(t, guard.Numeric.Value)
	}
	require.Len(t, tariff.Tiers[0].UnitPrices, 1)
	assert.Equal(t, "requested_seconds", tariff.Tiers[0].UnitPrices[0].Component)
	assert.Equal(t, "second", tariff.Tiers[0].UnitPrices[0].Unit)
	assert.InDelta(t, 15, tariff.Tiers[0].UnitPrices[0].AmountRub, 1e-12)
	assert.Equal(t, []effectiveUnitPrice{{Component: "base", Unit: "call", AmountRub: 150}}, tariff.Tiers[1].UnitPrices)
}

func TestEffectiveOperationTariffsProjectDisplayUnitsWithoutEvaluating(t *testing.T) {
	usageSchema := map[string]jsplugin.UsageFieldSchema{
		"seconds": {Type: "number", Unit: "second"},
		"clips":   {Type: "number", Unit: "count"},
		"tokens":  {Type: "number", Unit: "token"},
		"units":   {Type: "number", Unit: "credit"},
		"mode":    {Type: "enum", Enum: []string{"pro", "standard"}},
		"quality": {Type: "enum", Enum: []string{"high", "normal"}},
	}
	for _, test := range []struct {
		name       string
		expression string
		schema     map[string]jsplugin.UsageFieldSchema
		status     string
		prices     map[string]effectiveUnitPrice
	}{
		{name: "token zero preserved", expression: `tier("base", p * 2 + c * 0)`, status: "priced", prices: map[string]effectiveUnitPrice{
			"input":  {Component: "input", Unit: "million_tokens", AmountRub: 300},
			"output": {Component: "output", Unit: "million_tokens", AmountRub: 0},
		}},
		{name: "fixed zero preserved", expression: `tier("request", fixed(0))`, status: "priced", prices: map[string]effectiveUnitPrice{"base": {Component: "base", Unit: "call", AmountRub: 0}}},
		{name: "fixed request", expression: `tier("request", fixed(0.02))`, status: "priced", prices: map[string]effectiveUnitPrice{"base": {Component: "base", Unit: "call", AmountRub: 3}}},
		{name: "task units", expression: `tier("base", 0.1 + u("seconds") * 0.4 + u("clips") * 0.05 + u("tokens") * 9.8 / 1000000 + u("units") * 0.14)`, schema: usageSchema, status: "priced", prices: map[string]effectiveUnitPrice{
			"base":    {Component: "base", Unit: "call", AmountRub: 15},
			"seconds": {Component: "seconds", Unit: "second", AmountRub: 60},
			"clips":   {Component: "clips", Unit: "count", AmountRub: 7.5},
			"tokens":  {Component: "tokens", Unit: "million_tokens", AmountRub: 1470},
			"units":   {Component: "units", Unit: "credit", AmountRub: 21},
		}},
		{name: "provider cost is not tokens", expression: `tier("cost", provider_cost * 1000000)`, status: "request_dependent"},
		{name: "unknown quantity", expression: `tier("base", len * 2)`, status: "request_dependent"},
		{name: "nonlinear", expression: `tier("base", max(p, c) * 2)`, status: "request_dependent"},
		{name: "request multiplier", expression: `tier("base", p * 2) * (param("fast") == true ? 2 : 1)`, status: "request_dependent"},
		{name: "unknown condition", expression: `header("x-tier") == "fast" ? tier("fast", p * 2) : tier("base", p)`, status: "request_dependent"},
		{name: "other numeric condition", expression: `len == 1000 ? tier("exact", p * 2) : tier("base", p)`, status: "request_dependent"},
		{name: "task unknown key", expression: `tier("base", u("unknown") * 2)`, schema: usageSchema, status: "request_dependent"},
		{name: "task token scale missing", expression: `tier("base", u("tokens") * 2)`, schema: usageSchema, status: "request_dependent"},
		{name: "task reserve fixed rejected", expression: `tier("OpenRouter reserve", fixed(1))`, schema: usageSchema, status: "request_dependent"},
		{name: "negative literal", expression: `tier("base", p * -2)`, status: "request_dependent"},
		{name: "overflow literal", expression: `tier("base", p * 1e309)`, status: "request_dependent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := tieredPricing(test.expression)
			item.BillingUsageSchema = test.schema
			for _, source := range []string{model.BillingSourceConfiguredTariff, model.BillingSourceOpenRouterActualCost} {
				item.IndicativeBillingExprByEndpoint = map[constant.EndpointType]string{constant.EndpointTypeOpenAI: test.expression}
				tariff := buildOperationTariff(item, effectiveGroupPricing{EffectiveBillingRatio: 1.5, BillingSurface: "token"}, constant.EndpointTypeOpenAI, source)
				assert.Equal(t, test.status, tariff.Status, source)
				if test.status != "priced" {
					assert.Empty(t, tariff.Tiers)
					continue
				}
				require.Len(t, tariff.Tiers, 1)
				expectedSurface := "token"
				if test.schema != nil {
					expectedSurface = "task"
				} else if test.name == "fixed zero preserved" || test.name == "fixed request" {
					expectedSurface = "per_call"
				}
				assert.Equal(t, expectedSurface, tariff.BillingSurface)
				assert.Len(t, tariff.Tiers[0].UnitPrices, len(test.prices))
				for _, price := range tariff.Tiers[0].UnitPrices {
					expected, found := test.prices[price.Component]
					require.True(t, found, price.Component)
					assert.Equal(t, expected.Unit, price.Unit)
					assert.InDelta(t, expected.AmountRub, price.AmountRub, 1e-9)
				}
			}
		})
	}
}

func TestEffectiveOperationTariffsPreserveConditionsAndWholeTiers(t *testing.T) {
	group := effectiveGroupPricing{EffectiveBillingRatio: 1, BillingSurface: "token"}
	item := tieredPricing(`len <= 32000 ? tier("short", fixed(0.01)) : tier("long", p * 2 + c * 8)`)
	tariff := buildOperationTariff(item, group, constant.EndpointTypeOpenAI, model.BillingSourceConfiguredTariff)
	require.Equal(t, "priced", tariff.Status)
	require.Len(t, tariff.Tiers, 2)
	assert.Equal(t, "token", tariff.BillingSurface)
	assert.Equal(t, "token", group.BillingSurface)
	assert.Equal(t, &effectivePricingTierCondition{Kind: "context_length", Unit: "token", MaxValue: float(32000)}, tariff.Tiers[0].Condition)
	assert.Equal(t, &effectivePricingTierCondition{Kind: "context_length", Unit: "token", MinValue: float(32000)}, tariff.Tiers[1].Condition)
	assert.Equal(t, []effectiveUnitPrice{{Component: "base", Unit: "call", AmountRub: 1}}, tariff.Tiers[0].UnitPrices)
	assert.Len(t, tariff.Tiers[1].UnitPrices, 2)
	item = tieredPricing(`u("mode") == "pro" && u("quality") == "high" ? tier("pro", u("seconds") * 0.8) : u("mode") == "standard" ? tier("standard", u("seconds") * 0.4) : tier("other", u("seconds") * 0.6)`)
	item.BillingUsageSchema = map[string]jsplugin.UsageFieldSchema{
		"seconds": {Type: "number", Unit: "second"},
		"mode":    {Type: "enum", Enum: []string{"pro", "standard"}},
		"quality": {Type: "enum", Enum: []string{"high", "normal"}},
	}
	tariff = buildOperationTariff(item, group, constant.EndpointTypeOpenAIVideo, model.BillingSourceConfiguredTariff)
	require.Equal(t, "priced", tariff.Status)
	require.Len(t, tariff.Tiers, 3)
	first := tariff.Tiers[0].Condition
	require.NotNil(t, first)
	assert.Equal(t, "usage_enum", first.Kind)
	require.Len(t, first.Guards, 1)
	assert.Equal(t, map[string]string{"mode": "pro", "quality": "high"}, first.Guards[0].Values)
	assert.False(t, first.Guards[0].Negated)
	second := tariff.Tiers[1].Condition
	require.NotNil(t, second)
	require.Len(t, second.Guards, 2)
	assert.True(t, second.Guards[0].Negated)
	assert.False(t, second.Guards[1].Negated)
	assert.Equal(t, map[string]string{"mode": "standard"}, second.Guards[1].Values)
	third := tariff.Tiers[2].Condition
	require.NotNil(t, third)
	require.Len(t, third.Guards, 2)
	assert.True(t, third.Guards[0].Negated)
	assert.True(t, third.Guards[1].Negated)
	assert.Equal(t, "task", tariff.BillingSurface)
}

func TestEffectiveOperationTariffsKeepBatchAndSourcesSeparateWithoutMutatingCache(t *testing.T) {
	withTieredBillingConfig(t, nil, nil)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.batch_billing_expr": `{"operation-test":"tier(\"batch\", p * 0.5)"}`}))
	item := tieredPricing(`tier("sync", p * 2)`)
	item.ModelName = "operation-test"
	item.BillingSourcesByGroup = map[string]map[constant.EndpointType][]string{"default": {
		constant.EndpointTypeOpenAI: {model.BillingSourceConfiguredTariff, model.BillingSourceOpenRouterActualCost},
		model.PricingEndpointBatch:  {model.BillingSourceConfiguredTariff, model.BillingSourceOpenRouterActualCost},
	}}
	item.IndicativeBillingExprByEndpoint = map[constant.EndpointType]string{constant.EndpointTypeOpenAI: `tier("display", p * 1)`}
	legacy := effectivePricingResponse{Data: []effectivePricingModel{{ModelName: item.ModelName, GroupPrices: []effectiveGroupPricing{buildEffectiveGroupPricing(item, "default", 1, 1)}}}}
	enriched := withOperationTariffs(legacy, []model.Pricing{item})
	require.Nil(t, legacy.Data[0].GroupPrices[0].OperationTariffs)
	legacyBytes, err := common.Marshal(legacy)
	require.NoError(t, err)
	assert.NotContains(t, string(legacyBytes), "operation_tariffs")
	actual := enriched.Data[0].GroupPrices[0].OperationTariffs
	require.NotNil(t, actual)
	require.Len(t, *actual, 4)
	for _, tariff := range *actual {
		if tariff.EndpointType == model.PricingEndpointBatch && tariff.Source == model.BillingSourceOpenRouterActualCost {
			assert.Equal(t, "request_dependent", tariff.Status)
			assert.Empty(t, tariff.Tiers)
			continue
		}
		require.Equal(t, "priced", tariff.Status)
		require.Len(t, tariff.Tiers, 1)
		require.Len(t, tariff.Tiers[0].UnitPrices, 1)
		expected := 200.0
		if tariff.EndpointType == model.PricingEndpointBatch {
			expected = 50
		} else if tariff.Source == model.BillingSourceOpenRouterActualCost {
			expected = 100
		}
		assert.Equal(t, expected, tariff.Tiers[0].UnitPrices[0].AmountRub)
	}
	item.BillingSourcesByGroup = nil
	missing := withOperationTariffs(legacy, []model.Pricing{item})
	missingBytes, err := common.Marshal(missing)
	require.NoError(t, err)
	assert.Contains(t, string(missingBytes), `"operation_tariffs":null`)
	tariff := buildOperationTariff(item, effectiveGroupPricing{EffectiveBillingRatio: math.Inf(1)}, constant.EndpointTypeOpenAI, model.BillingSourceConfiguredTariff)
	assert.Equal(t, "unavailable", tariff.Status)
	assert.Empty(t, tariff.Tiers)
}

const prodFXFactor = 0.854594

func tieredPricing(expr string) model.Pricing {
	return model.Pricing{BillingMode: billing_setting.BillingModeTieredExpr, BillingExpr: expr}
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

func TestOpenRouterIndicativeTariffsKeepGroupEndpointAndExpressionOwnership(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "openrouter only", true: "mixed direct and openrouter"}[mixed], func(t *testing.T) {
			sources := []string{model.BillingSourceOpenRouterActualCost}
			if mixed {
				sources = append(sources, model.BillingSourceConfiguredTariff)
			}
			item := model.Pricing{
				ModelName: "vendor-independent-model", EnableGroup: []string{"all"}, ModelRatio: 9, CompletionRatio: 2,
				BillingSourcesByGroup:           map[string]map[constant.EndpointType][]string{"all": {constant.EndpointTypeOpenAI: sources, model.PricingEndpointBatch: sources}},
				IndicativeBillingExprByEndpoint: map[constant.EndpointType]string{constant.EndpointTypeOpenAI: `tier("display", p * 2)`},
			}
			result := buildEffectivePricingModel("default", map[string]string{"default": ""}, prodFXFactor, item)
			require.Len(t, result.GroupPrices, 1)
			group := result.GroupPrices[0]
			assert.Equal(t, sources, group.BillingSourcesByEndpoint[constant.EndpointTypeOpenAI])
			assert.Equal(t, sources, group.BillingSourcesByEndpoint[model.PricingEndpointBatch])
			require.Len(t, group.Tiers, 1) // Public legacy fields remain compatible.
			sync := group.OpenRouterIndicativeTariffs[constant.EndpointTypeOpenAI]
			require.NotNil(t, sync)
			require.Len(t, sync.Tiers, 1)
			assert.InDelta(t, group.EffectiveBillingRatio*200, sync.Tiers[0].UnitPrices[0].AmountRub, 1e-9)
			batch := group.OpenRouterIndicativeTariffs[model.PricingEndpointBatch]
			require.NotNil(t, batch)
			assert.Equal(t, "unavailable", batch.Status)
			assert.Equal(t, []string{"missing_display_expression"}, batch.Limitations)
			assert.Empty(t, batch.Tiers)
		})
	}
}

func TestOpenRouterMissingOrCostOnlyDisplayNeverUsesLegacyAmounts(t *testing.T) {
	for _, expression := range []string{"", `tier("cost", provider_cost * 1000000)`} {
		t.Run(expression, func(t *testing.T) {
			item := model.Pricing{EnableGroup: []string{"default"}, ModelRatio: 99, CompletionRatio: 9,
				BillingSourcesByGroup:           map[string]map[constant.EndpointType][]string{"default": {constant.EndpointTypeOpenAI: {model.BillingSourceOpenRouterActualCost}}},
				IndicativeBillingExprByEndpoint: map[constant.EndpointType]string{constant.EndpointTypeOpenAI: expression},
			}
			group := buildEffectivePricingModel("default", map[string]string{"default": ""}, 1, item).GroupPrices[0]
			tariff := group.OpenRouterIndicativeTariffs[constant.EndpointTypeOpenAI]
			require.NotNil(t, tariff)
			assert.Empty(t, tariff.Tiers)
			assert.Empty(t, tariff.UnitPrices)
			assert.False(t, tariff.IsFree)
			if expression == "" {
				assert.Equal(t, "unavailable", tariff.Status)
			} else {
				assert.Equal(t, "formula", tariff.Status)
			}
		})
	}
}

func pricingPageFixture() effectivePricingPageContext {
	return effectivePricingPageContext{
		metadata:     effectivePricingResponse{SchemaVersion: "v1", UserGroup: "default", Currency: "RUB", PriceKind: "current_tariff", PriceScope: "user_group", AutoGroups: []string{}, Accounting: effectiveAccounting{QuotaPerUnit: 500000, RubPerUnit: 100, QuotaPerRub: 5000}, FX: service.BillingFXBasis{Factor: 1, USDRate: 100, Source: "cbr", PublicationVersion: 1, EffectiveAt: 1, FetchedAt: 1}},
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
}

func TestEffectivePricingPageSelectionAndProjectionBound(t *testing.T) {
	context := pricingPageFixture()
	query, err := parseEffectivePricingPageQuery(url.Values{"limit": {"1"}, "offset": {"1"}}, true)
	require.NoError(t, err)
	for range 2 {
		calls := 0
		page := buildEffectivePricingPage(context, query, func(item model.Pricing) effectivePricingModel { calls++; return context.project(item) })
		assert.Equal(t, 4, page.TotalCount)
		require.Len(t, page.Data, 1)
		assert.Equal(t, "premium-only", page.Data[0].ModelName)
		assert.Equal(t, 1, calls)
		assert.Equal(t, []effectivePricingCreator{{ID: 2, Name: "Anthropic"}, {ID: 1, Name: "OpenAI"}}, page.Creators)
	}
	for _, values := range []url.Values{{"limit": {"0"}}, {"offset": {"9007199254740991"}}} {
		query, err = parseEffectivePricingPageQuery(values, true)
		require.NoError(t, err)
		page := buildEffectivePricingPage(context, query, func(model.Pricing) effectivePricingModel {
			t.Fatal("empty page projected tariffs")
			return effectivePricingModel{}
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

func TestEffectivePricingPagePublicVisibilityAndLegacyWire(t *testing.T) {
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
		legacy := withOperationTariffs(effectivePricingResponse{Data: []effectivePricingModel{buildEffectivePricingModelWithRatios(context.usableGroups, context.groupRatios, 1, context.pricing[slices.IndexFunc(context.pricing, func(item model.Pricing) bool { return item.ModelName == row.ModelName })])}}, context.pricing)
		expected, err := common.Marshal(legacy.Data[0])
		require.NoError(t, err)
		actual, err := common.Marshal(row.effectivePricingModel)
		require.NoError(t, err)
		assert.JSONEq(t, string(expected), string(actual))
		assert.Contains(t, string(actual), `"operation_tariffs":null`)
		assert.NotContains(t, string(actual), `"billing_sources_by_endpoint"`)
	}
	context.groupRatios["default"] = -1
	assert.Empty(t, buildEffectivePricingPage(context, query, context.project).Data)
	context.pricing[0].BillingSourcesByGroup = map[string]map[constant.EndpointType][]string{"default": {constant.EndpointTypeOpenAI: {"unknown"}}}
	page = buildEffectivePricingPage(context, query, context.project)
	require.Len(t, page.Data, 1)
	encoded, err := common.Marshal(page)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"operation_tariffs":[]`)
	assert.Equal(t, "unavailable", page.Data[0].GroupPrices[0].Status)
	context.usingGroup = ""
	context.usableGroups["premium"] = "Premium"
	assert.Equal(t, 4, buildEffectivePricingPage(context, query, context.project).TotalCount)
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
	query.pricingUnit = "CALLS"
	query.limit = 0
	calls := 0
	page = buildEffectivePricingPage(context, query, func(item model.Pricing) effectivePricingModel { calls++; return context.project(item) })
	assert.Equal(t, 1, page.TotalCount)
	assert.Empty(t, page.Data)
	assert.Equal(t, 4, calls)
	batch := []effectiveOperationTariff{{EndpointType: model.PricingEndpointBatch, BillingSurface: "per_call", Status: "unavailable"}, {EndpointType: constant.EndpointTypeOpenAI, BillingSurface: "token", Status: "priced", Tiers: []effectivePricingTier{{UnitPrices: []effectiveUnitPrice{{Component: "input", Unit: "million_tokens", AmountRub: 3}}}}}}
	unit, price := effectivePricingPageKeys(effectivePricingModel{GroupPrices: []effectiveGroupPricing{{OperationTariffs: &batch}}}, "INPUT_PRICE")
	assert.Equal(t, "CALLS", unit)
	require.NotNil(t, price)
	assert.Equal(t, 3.0, *price)
	empty := []effectiveOperationTariff{}
	unit, price = effectivePricingPageKeys(effectivePricingModel{GroupPrices: []effectiveGroupPricing{{OperationTariffs: &empty, BillingSurface: "token"}}}, "INPUT_PRICE")
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
