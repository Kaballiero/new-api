package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
