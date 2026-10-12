package billingexpr

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskUsageExpressionUsesFactsAndTaskQuotaConversion(t *testing.T) {
	expression := `tier("1080p", u("seconds") * (u("resolution") == "1080p" ? 0.4 : 0.2))`
	cost, trace, err := RunExprWithRequest(expression, TokenParams{}, RequestInput{Usage: map[string]any{"seconds": 10.0, "resolution": "1080p"}})
	require.NoError(t, err)
	assert.Equal(t, 4.0, cost)
	assert.Equal(t, "1080p", trace.MatchedTier)
	result, err := ComputeTieredQuotaWithRequest(&BillingSnapshot{ExprString: expression, ExprHash: ExprHashString(expression), GroupRatio: 2, QuotaPerUnit: 500000, ExprVersion: 1, TaskUsageBilling: true}, TokenParams{}, RequestInput{Usage: map[string]any{"seconds": 10.0, "resolution": "1080p"}})
	require.NoError(t, err)
	assert.Equal(t, 4_000_000, result.ActualQuotaAfterGroup)
}

func TestTaskDisplayRejectsExcessiveExpansionAndInvalidSchema(t *testing.T) {
	schema := map[string]DisplayUsageField{"seconds": {Unit: "second"}}
	terms := make([]string, 9)
	for index := range terms {
		key := fmt.Sprintf("mode%d", index)
		schema[key] = DisplayUsageField{Enum: []string{"a", "b"}}
		terms[index] = fmt.Sprintf(`(u(%q) == "a" ? 1 : 2)`, key)
	}
	tiers, err := EnumerateDisplayTiers(`tier("base", `+strings.Join(terms, " * ")+`)`, schema)
	require.ErrorContains(t, err, "branch limit")
	assert.Empty(t, tiers)
	condition := `u("mode0") == "a"`
	for index := range 40 {
		operator := "&&"
		if index%2 == 0 {
			operator = "||"
		}
		condition = "(" + condition + " " + operator + ` u("mode0") == "a")`
	}
	tiers, err = EnumerateDisplayTiers(`tier("base", (`+condition+`) ? 1 : 2)`, schema)
	require.ErrorContains(t, err, "work limit")
	assert.Empty(t, tiers)
	tiers, err = EnumerateDisplayTiers(`tier("base", `+strings.Repeat("1 + (", 70)+"1"+strings.Repeat(")", 70)+`)`, schema)
	require.ErrorContains(t, err, "depth limit")
	assert.Empty(t, tiers)
	for _, field := range []DisplayUsageField{{Unit: "megapixel"}, {Unit: "second", Enum: []string{"a"}}} {
		tiers, err = EnumerateDisplayTiers(`tier("base", 2 * u("quantity"))`, map[string]DisplayUsageField{"quantity": field})
		assert.Error(t, err)
		assert.Empty(t, tiers)
	}
}

func TestTaskDisplayProjectsFiniteConstantsAndEnumAlternatives(t *testing.T) {
	schema := map[string]DisplayUsageField{
		"seconds": {Unit: "second"}, "videos": {Unit: "count"},
		"creativity": {Enum: []string{"default", "0", "1"}},
		"resolution": {Enum: []string{"default", "720p", "1080p"}},
	}
	for _, test := range []struct {
		expression string
		amounts    []float64
	}{
		{`tier("base", 15 * 0.03)`, []float64{0.45}},
		{`tier("base", 180 * 0.05)`, []float64{9}},
		{`tier("base", max(0.56, 30 * 0.28))`, []float64{8.4}},
		{`tier("base", 20 * 14.4 * (u("creativity") == "1" ? 0.105 : 0.075))`, []float64{30.24, 21.6}},
	} {
		t.Run(test.expression, func(t *testing.T) {
			tiers, err := EnumerateDisplayTiers(test.expression, schema)
			require.NoError(t, err)
			require.Len(t, tiers, len(test.amounts))
			for index, tier := range tiers {
				require.Len(t, tier.Prices, 1)
				assert.Equal(t, "call", tier.Prices[0].Unit)
				assert.InDelta(t, test.amounts[index], tier.Prices[0].AmountUSD, 1e-12)
			}
		})
	}
	expression := `tier("base", (0.1 + u("seconds") * 0.2 + 2 * u("videos")) * (u("resolution") == "720p" || u("resolution") == "1080p" ? 2 : 1))`
	tiers, err := EnumerateDisplayTiers(expression, schema)
	require.NoError(t, err)
	require.Len(t, tiers, 3)
	for _, resolution := range schema["resolution"].Enum {
		matched := 0
		for _, tier := range tiers {
			applies := true
			for _, guard := range tier.UsageGuards {
				for _, value := range guard.Values {
					applies = applies && ((value == resolution) != guard.Negated)
				}
			}
			if !applies {
				continue
			}
			matched++
			price := 0.0
			facts := map[string]any{"seconds": 4.0, "videos": 2.0, "resolution": resolution}
			for _, component := range tier.Prices {
				quantity := 1.0
				if component.Component != "base" {
					quantity = facts[component.Component].(float64)
				}
				price += quantity * component.AmountUSD
			}
			expected, _, err := RunExprWithRequest(expression, TokenParams{}, RequestInput{Usage: facts})
			require.NoError(t, err)
			assert.InDelta(t, expected, price, 1e-12)
		}
		assert.Equal(t, 1, matched, resolution)
	}
	for _, expression := range []string{
		`tier("base", provider_cost * 1000000)`,
		`tier("base", u("seconds") * u("videos"))`,
		`tier("base", 1 / 0)`,
		`tier("base", 1e308 * 1e308)`,
		`tier("base", u("seconds") * -1)`,
		`tier("base", max(u("seconds"), 1))`,
		`tier("base", u("unknown") * 1)`,
	} {
		tiers, err := EnumerateDisplayTiers(expression, schema)
		assert.Error(t, err, expression)
		assert.Empty(t, tiers, expression)
	}
}

func TestTaskDisplayNumericDefaultsRetainCompleteConditionalPrice(t *testing.T) {
	schema := map[string]DisplayUsageField{
		"requested_seconds": {Unit: "second"}, "videos": {Unit: "count"},
		"resolution": {Enum: []string{"default", "720p", "1080p"}},
		"audio":      {Enum: []string{"default", "enabled", "disabled"}},
	}
	expression := `tier("base", ((u("requested_seconds") > 0 ? u("requested_seconds") : 15) + 1.0 / 24 + 30 * u("videos")) * (u("videos") > 0 ? 0.1 : (u("resolution") == "720p" || u("audio") == "disabled" ? 0.2 : 0.4)))`
	tiers, err := EnumerateDisplayTiers(expression, schema)
	require.NoError(t, err)
	for _, seconds := range []float64{0, 0.5, 4, 3600} {
		for _, videos := range []float64{0, 1, 128} {
			for _, resolution := range schema["resolution"].Enum {
				for _, audio := range schema["audio"].Enum {
					facts := map[string]any{"requested_seconds": seconds, "videos": videos, "resolution": resolution, "audio": audio}
					expected, _, err := RunExprWithRequest(expression, TokenParams{}, RequestInput{Usage: facts})
					require.NoError(t, err)
					matched := 0
					for _, tier := range tiers {
						applies := true
						for _, guard := range tier.UsageGuards {
							condition := true
							for key, value := range guard.Values {
								condition = condition && facts[key] == value
							}
							if guard.Numeric != nil {
								condition = condition && facts[guard.Numeric.Field].(float64) > guard.Numeric.Value
							}
							applies = applies && condition != guard.Negated
						}
						if !applies {
							continue
						}
						matched++
						actual := 0.0
						for _, price := range tier.Prices {
							quantity := 1.0
							if price.Component != "base" {
								quantity = facts[price.Component].(float64)
							}
							actual += quantity * price.AmountUSD
							if seconds == 0 {
								assert.NotEqual(t, "requested_seconds", price.Component)
							}
						}
						assert.InDelta(t, expected, actual, 1e-9, facts)
					}
					assert.Equal(t, 1, matched, facts)
				}
			}
		}
	}
	for _, condition := range []string{`u("missing") > 0`, `u("audio") > 0`, `u("videos") > -1`, `u("videos") > 1e309`, `u("videos") > u("requested_seconds")`, `u("videos") >= 1`} {
		tiers, err := EnumerateDisplayTiers(`tier("base", (`+condition+`) ? 1 : 2)`, schema)
		assert.Error(t, err, condition)
		assert.Empty(t, tiers)
	}
	threshold, err := EnumerateDisplayTiers(`tier("base", u("requested_seconds") > 0.5 ? 2 : 1)`, schema)
	require.NoError(t, err)
	require.Len(t, threshold, 2)
	assert.Equal(t, 0.5, threshold[0].UsageGuards[0].Numeric.Value)
	assert.False(t, threshold[0].UsageGuards[0].Negated)
	assert.True(t, threshold[1].UsageGuards[0].Negated)
}

func TestTaskDisplayPreservesGroupedNegationAndFractionalThreshold(t *testing.T) {
	schema := map[string]DisplayUsageField{
		"seconds": {Unit: "second"}, "units": {Unit: "credit"}, "clips": {Unit: "count"},
		"mode": {Enum: []string{"a", "b"}}, "audio": {Enum: []string{"enabled", "disabled"}},
	}
	for _, expression := range []string{
		`u("mode") == "a" && u("audio") == "enabled" ? tier("yes", u("seconds") * 2) : tier("no", u("seconds") * 1)`,
		`tier("base", (u("mode") == "a" && u("audio") == "enabled" ? 2.0 : 1.0) * (u("seconds") > 0.5 ? u("seconds") : 15.0))`,
		`tier("base", (u("mode") == "a" || u("audio") == "enabled" ? 2 : 1) * (u("units") > 0.5 ? u("units") : 15) + u("clips"))`,
	} {
		t.Run(expression, func(t *testing.T) {
			tiers, err := EnumerateDisplayTiers(expression, schema)
			require.NoError(t, err)
			for _, mode := range schema["mode"].Enum {
				for _, audio := range schema["audio"].Enum {
					for _, quantity := range []float64{0, 0.49, 0.5, 0.51, 1.5} {
						facts := map[string]any{"mode": mode, "audio": audio, "seconds": quantity, "units": quantity, "clips": 2.0}
						expected, _, err := RunExprWithRequest(expression, TokenParams{}, RequestInput{Usage: facts})
						require.NoError(t, err)
						matched := 0
						for _, tier := range tiers {
							applies := true
							for _, guard := range tier.UsageGuards {
								condition := true
								for key, value := range guard.Values {
									condition = condition && facts[key] == value
								}
								if guard.Numeric != nil {
									assert.Empty(t, guard.Values)
									assert.Equal(t, schema[guard.Numeric.Field].Unit, guard.Numeric.Unit)
									condition = facts[guard.Numeric.Field].(float64) > guard.Numeric.Value
								}
								applies = applies && condition != guard.Negated
							}
							if !applies {
								continue
							}
							matched++
							actual := 0.0
							for _, price := range tier.Prices {
								quantity := 1.0
								if price.Component != "base" {
									quantity = facts[price.Component].(float64)
								}
								actual += quantity * price.AmountUSD
							}
							assert.InDelta(t, expected, actual, 1e-12, facts)
						}
						assert.Equal(t, 1, matched, facts)
					}
				}
			}
		})
	}
	assert.True(t, taskDisplayGuardsPossible([]DisplayUsageGuard{
		{Values: map[string]string{"mode": "a", "audio": "enabled"}, Negated: true},
		{Values: map[string]string{"mode": "a", "audio": "disabled"}},
	}, schema))
}

func TestTaskDisplayPreservesLargeCanonicalTariff(t *testing.T) {
	schema := map[string]DisplayUsageField{"seconds": {Unit: "second"}}
	expression := `tier("base", u("seconds") * 0.4)`
	const enumFields = 9
	const canonicalLeaves = 512
	for index := range enumFields {
		key := fmt.Sprintf("mode%d", index)
		schema[key] = DisplayUsageField{Enum: []string{"a", "b"}}
		expression = fmt.Sprintf(`u(%q) == "a" ? (%s) : (%s)`, key, expression, expression)
	}
	tiers, err := EnumerateDisplayTiers(expression, schema)
	require.NoError(t, err)
	require.Len(t, tiers, canonicalLeaves)
	for _, tier := range tiers {
		assert.Equal(t, "base", tier.Label)
		assert.Equal(t, []DisplayPrice{{Component: "seconds", Unit: "second", AmountUSD: 0.4}}, tier.Prices)
		assert.Len(t, tier.UsageGuards, enumFields)
	}
}
