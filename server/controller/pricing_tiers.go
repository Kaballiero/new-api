package controller

import (
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

type effectivePricingTier = pricingdomain.EffectivePricingTier

// effectivePricingTierCondition describes what selects a tier without exposing
// expression text. Numeric kinds carry an exclusive MinValue and an inclusive
// MaxValue in tokens; time_of_day carries half-open [start, end) hour windows.
type effectivePricingTierCondition = pricingdomain.EffectivePricingTierCondition

type effectivePricingTimeWindow = pricingdomain.EffectivePricingTimeWindow

const hoursPerDay = 24

// effectivePricingComponents maps expression variables onto the component
// vocabulary unit_prices already publishes, in display order. Variables outside
// the table are published under their own name.
var effectivePricingComponents = []struct{ variable, component string }{
	{"p", "input"},
	{"c", "output"},
	{"cr", "cache_read"},
	{"cc", "cache_write"},
	{"cc1h", "cache_write_1h"},
	{"img", "image_input"},
	{"img_o", "image_output"},
	{"ai", "audio_input"},
	{"ao", "audio_output"},
}

var tierConditionKinds = map[string]string{
	"len": "context_length",
	"p":   "input_tokens",
	"c":   "output_tokens",
}

func orderedComponentVariables(referenced map[string]bool) []string {
	return pricingdomain.OrderedComponentVariables(referenced)
}

func componentName(variable string) string {
	return pricingdomain.ComponentName(variable)
}

func classifyTierConditions(conditions []billingexpr.TierCondition) *effectivePricingTierCondition {
	return pricingdomain.ClassifyTierConditions(conditions)
}

// tokenBoundsCondition folds a chain of comparisons on one token variable into
// (min, max]. Token counts are integers, so strict comparisons shift by one.
func tokenBoundsCondition(conditions []billingexpr.TierCondition) (*effectivePricingTierCondition, bool) {
	return pricingdomain.TokenBoundsCondition(conditions)
}

func flattenConjunction(condition billingexpr.TierCondition, comparisons *[]billingexpr.TierCondition) bool {
	return pricingdomain.FlattenConjunction(condition, comparisons)
}

func negateComparison(op string) string {
	return pricingdomain.
		NegateComparison(op)
}

// timeOfDayCondition evaluates hour("<zone>") guards for each hour of the day
// and reports the hours in which the tier applies as [start, end) windows, so
// an else branch yields the exact complement of its sibling.
func timeOfDayCondition(conditions []billingexpr.TierCondition) (*effectivePricingTierCondition, bool) {
	return pricingdomain.TimeOfDayCondition(conditions)
}

func hourMask(condition billingexpr.TierCondition, zone *string) (uint32, bool) {
	return pricingdomain.
		HourMask(condition, zone)
}

func compareNumbers(left float64, op string, right float64) bool {
	return pricingdomain.CompareNumbers(left, op, right)
}
