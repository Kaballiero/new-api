package controller

import (
	"math"
	"sort"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

type effectivePricingTier struct {
	Label      string                         `json:"label,omitempty"`
	Condition  *effectivePricingTierCondition `json:"condition,omitempty"`
	UnitPrices []effectiveUnitPrice           `json:"unit_prices"`
}

// effectivePricingTierCondition describes what selects a tier without exposing
// expression text. Numeric kinds carry an exclusive MinValue and an inclusive
// MaxValue in tokens; time_of_day carries half-open [start, end) hour windows.
type effectivePricingTierCondition struct {
	Kind        string                       `json:"kind"`
	Unit        string                       `json:"unit,omitempty"`
	MinValue    *float64                     `json:"min_value,omitempty"`
	MaxValue    *float64                     `json:"max_value,omitempty"`
	TimeZone    string                       `json:"time_zone,omitempty"`
	TimeWindows []effectivePricingTimeWindow `json:"time_windows,omitempty"`
}

type effectivePricingTimeWindow struct {
	StartHour int `json:"start_hour"`
	EndHour   int `json:"end_hour"`
}

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

func (g *effectiveGroupPricing) setTiers(tiers []effectivePricingTier) {
	g.Tiers = tiers
	g.IsFree = len(tiers) > 0
	for _, tier := range tiers {
		for _, price := range tier.UnitPrices {
			if price.AmountRub != 0 {
				g.IsFree = false
			}
		}
	}
}

// tokenExpressionTiers projects a token expression onto ready-to-display tiers.
// A component is listed iff some tier of the expression references it, so a
// zero amount means "free" while absence means "not priced". Any tier whose
// body has no linear price makes the whole projection empty: a partial table
// would be a wrong price.
func tokenExpressionTiers(expression string, outputToRub float64) []effectivePricingTier {
	tiers, err := billingexpr.EnumerateTiers(expression)
	if err != nil {
		return []effectivePricingTier{}
	}
	referenced := make(map[string]bool)
	for _, tier := range tiers {
		if !tier.Linear {
			return []effectivePricingTier{}
		}
		for variable := range tier.Coefficients {
			referenced[variable] = true
		}
	}
	variables := orderedComponentVariables(referenced)

	result := make([]effectivePricingTier, 0, len(tiers))
	for _, tier := range tiers {
		prices := make([]effectiveUnitPrice, 0, len(variables))
		for _, variable := range variables {
			prices = append(prices, effectiveUnitPrice{
				Component: componentName(variable),
				Unit:      "million_tokens",
				AmountRub: tier.Coefficients[variable] * outputToRub * 1_000_000,
			})
		}
		result = append(result, effectivePricingTier{Label: tier.Label, Condition: classifyTierConditions(tier.Conditions), UnitPrices: prices})
	}
	return result
}

func orderedComponentVariables(referenced map[string]bool) []string {
	ordered := make([]string, 0, len(referenced))
	rest := make([]string, 0)
	for _, entry := range effectivePricingComponents {
		if referenced[entry.variable] {
			ordered = append(ordered, entry.variable)
		}
	}
	for variable := range referenced {
		if componentName(variable) == variable {
			rest = append(rest, variable)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

func componentName(variable string) string {
	for _, entry := range effectivePricingComponents {
		if entry.variable == variable {
			return entry.component
		}
	}
	return variable
}

func classifyTierConditions(conditions []billingexpr.TierCondition) *effectivePricingTierCondition {
	if len(conditions) == 0 {
		return nil
	}
	if bounds, ok := tokenBoundsCondition(conditions); ok {
		return bounds
	}
	if windows, ok := timeOfDayCondition(conditions); ok {
		return windows
	}
	return &effectivePricingTierCondition{Kind: "other"}
}

// tokenBoundsCondition folds a chain of comparisons on one token variable into
// (min, max]. Token counts are integers, so strict comparisons shift by one.
func tokenBoundsCondition(conditions []billingexpr.TierCondition) (*effectivePricingTierCondition, bool) {
	var comparisons []billingexpr.TierCondition
	for _, condition := range conditions {
		if !flattenConjunction(condition, &comparisons) {
			return nil, false
		}
	}
	result := &effectivePricingTierCondition{Unit: "token"}
	variable := comparisons[0].Var
	for _, comparison := range comparisons {
		if comparison.Var != variable || comparison.Arg != "" || tierConditionKinds[variable] == "" {
			return nil, false
		}
		op := comparison.Op
		if comparison.Negated {
			op = negateComparison(op)
		}
		value := comparison.Value
		if (op == "<" || op == ">=") && value == math.Trunc(value) {
			value--
		}
		switch op {
		case "<", "<=":
			if result.MaxValue == nil || value < *result.MaxValue {
				result.MaxValue = &value
			}
		case ">", ">=":
			if result.MinValue == nil || value > *result.MinValue {
				result.MinValue = &value
			}
		default:
			return nil, false
		}
	}
	result.Kind = tierConditionKinds[variable]
	return result, true
}

func flattenConjunction(condition billingexpr.TierCondition, comparisons *[]billingexpr.TierCondition) bool {
	if len(condition.Operands) == 0 {
		*comparisons = append(*comparisons, condition)
		return true
	}
	if condition.Op != "&&" || condition.Negated {
		return false
	}
	for _, operand := range condition.Operands {
		if !flattenConjunction(operand, comparisons) {
			return false
		}
	}
	return true
}

func negateComparison(op string) string {
	switch op {
	case "<":
		return ">="
	case "<=":
		return ">"
	case ">":
		return "<="
	case ">=":
		return "<"
	case "==":
		return "!="
	case "!=":
		return "=="
	default:
		return op
	}
}

// timeOfDayCondition evaluates hour("<zone>") guards for each hour of the day
// and reports the hours in which the tier applies as [start, end) windows, so
// an else branch yields the exact complement of its sibling.
func timeOfDayCondition(conditions []billingexpr.TierCondition) (*effectivePricingTierCondition, bool) {
	zone := ""
	hours := uint32(1<<hoursPerDay - 1)
	for _, condition := range conditions {
		mask, ok := hourMask(condition, &zone)
		if !ok {
			return nil, false
		}
		hours &= mask
	}
	result := &effectivePricingTierCondition{Kind: "time_of_day", TimeZone: zone, TimeWindows: []effectivePricingTimeWindow{}}
	for hour := 0; hour < hoursPerDay; hour++ {
		if hours&(1<<hour) == 0 {
			continue
		}
		end := hour + 1
		for end < hoursPerDay && hours&(1<<end) != 0 {
			end++
		}
		result.TimeWindows = append(result.TimeWindows, effectivePricingTimeWindow{StartHour: hour, EndHour: end})
		hour = end
	}
	return result, true
}

func hourMask(condition billingexpr.TierCondition, zone *string) (uint32, bool) {
	const allHours = uint32(1<<hoursPerDay - 1)
	var mask uint32
	switch condition.Op {
	case "&&", "||":
		mask = allHours
		if condition.Op == "||" {
			mask = 0
		}
		for _, operand := range condition.Operands {
			operandMask, ok := hourMask(operand, zone)
			if !ok {
				return 0, false
			}
			if condition.Op == "&&" {
				mask &= operandMask
			} else {
				mask |= operandMask
			}
		}
	default:
		if condition.Var != "hour" || (*zone != "" && condition.Arg != *zone) {
			return 0, false
		}
		*zone = condition.Arg
		for hour := 0; hour < hoursPerDay; hour++ {
			if compareNumbers(float64(hour), condition.Op, condition.Value) {
				mask |= 1 << hour
			}
		}
	}
	if condition.Negated {
		mask ^= allHours
	}
	return mask, true
}

func compareNumbers(left float64, op string, right float64) bool {
	switch op {
	case "<":
		return left < right
	case "<=":
		return left <= right
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "==":
		return left == right
	case "!=":
		return left != right
	default:
		return false
	}
}
