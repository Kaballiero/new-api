package controller

import (
	"slices"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

func pricingGuardKey(guard billingexpr.DisplayUsageGuard) string {
	raw, _ := common.Marshal(guard)
	return string(raw)
}

func normalizePricingGuards(guards []billingexpr.DisplayUsageGuard) ([]billingexpr.DisplayUsageGuard, bool) {
	facts := make(map[string]string)
	lower, upper := make(map[string]billingexpr.DisplayUsageGuard), make(map[string]billingexpr.DisplayUsageGuard)
	for _, guard := range guards {
		if guard.Numeric != nil || guard.Negated {
			continue
		}
		for field, value := range guard.Values {
			if previous, found := facts[field]; found && previous != value {
				return nil, false
			}
			facts[field] = value
		}
	}
	result := make([]billingexpr.DisplayUsageGuard, 0, len(guards))
	for _, guard := range guards {
		if guard.Numeric != nil {
			if guard.Numeric.Operator != "gt" || len(guard.Values) != 0 {
				result = append(result, guard)
				continue
			}
			key := guard.Numeric.Field + ":" + guard.Numeric.Unit
			if guard.Negated {
				if previous, found := upper[key]; !found || guard.Numeric.Value < previous.Numeric.Value {
					upper[key] = guard
				}
			} else if previous, found := lower[key]; !found || guard.Numeric.Value > previous.Numeric.Value {
				lower[key] = guard
			}
			continue
		}
		if !guard.Negated {
			continue
		}
		values := make(map[string]string)
		redundant := false
		for field, value := range guard.Values {
			if known, found := facts[field]; found {
				if known != value {
					redundant = true
					break
				}
			} else {
				values[field] = value
			}
		}
		if redundant {
			continue
		}
		if len(values) == 0 {
			return nil, false
		}
		result = append(result, billingexpr.DisplayUsageGuard{Values: values, Negated: true})
	}
	for field, value := range facts {
		result = append(result, billingexpr.DisplayUsageGuard{Values: map[string]string{field: value}})
	}
	for key, guard := range lower {
		if maximum, found := upper[key]; found && guard.Numeric.Value >= maximum.Numeric.Value {
			return nil, false
		}
		result = append(result, guard)
	}
	for _, guard := range upper {
		result = append(result, guard)
	}
	sort.Slice(result, func(left, right int) bool { return pricingGuardKey(result[left]) < pricingGuardKey(result[right]) })
	return slices.CompactFunc(result, func(left, right billingexpr.DisplayUsageGuard) bool {
		return pricingGuardKey(left) == pricingGuardKey(right)
	}), true
}

func pricingGuardSubset(left, right []billingexpr.DisplayUsageGuard) bool {
	for _, guard := range left {
		if !slices.ContainsFunc(right, func(candidate billingexpr.DisplayUsageGuard) bool {
			return pricingGuardKey(candidate) == pricingGuardKey(guard)
		}) {
			return false
		}
	}
	return true
}

func complementaryPricingBranches(left, right []billingexpr.DisplayUsageGuard) ([]billingexpr.DisplayUsageGuard, bool) {
	if len(left) != len(right) {
		return nil, false
	}
	commonGuards := make([]billingexpr.DisplayUsageGuard, 0, len(left))
	var differentLeft, differentRight []billingexpr.DisplayUsageGuard
	for _, guard := range left {
		if slices.ContainsFunc(right, func(candidate billingexpr.DisplayUsageGuard) bool {
			return pricingGuardKey(candidate) == pricingGuardKey(guard)
		}) {
			commonGuards = append(commonGuards, guard)
		} else {
			differentLeft = append(differentLeft, guard)
		}
	}
	for _, guard := range right {
		if !slices.ContainsFunc(left, func(candidate billingexpr.DisplayUsageGuard) bool {
			return pricingGuardKey(candidate) == pricingGuardKey(guard)
		}) {
			differentRight = append(differentRight, guard)
		}
	}
	if len(differentLeft) != 1 || len(differentRight) != 1 {
		return nil, false
	}
	positive, negative := differentLeft[0], differentRight[0]
	if positive.Negated == negative.Negated {
		return nil, false
	}
	positive.Negated, negative.Negated = false, false
	if pricingGuardKey(positive) != pricingGuardKey(negative) {
		return nil, false
	}
	return commonGuards, true
}

func simplifyPricingAlternatives(alternatives [][]billingexpr.DisplayUsageGuard) [][]billingexpr.DisplayUsageGuard {
	for {
		changed := false
		for left := 0; left < len(alternatives) && !changed; left++ {
			for right := left + 1; right < len(alternatives); right++ {
				if pricingGuardSubset(alternatives[left], alternatives[right]) {
					alternatives = slices.Delete(alternatives, right, right+1)
					changed = true
					break
				}
				if pricingGuardSubset(alternatives[right], alternatives[left]) {
					alternatives = slices.Delete(alternatives, left, left+1)
					changed = true
					break
				}
				if commonGuards, complementary := complementaryPricingBranches(alternatives[left], alternatives[right]); complementary {
					alternatives[left] = commonGuards
					alternatives = slices.Delete(alternatives, right, right+1)
					changed = true
					break
				}
			}
		}
		if !changed {
			return alternatives
		}
	}
}

func pricingAlternativeCondition(alternatives [][]billingexpr.DisplayUsageGuard) *effectivePricingTierCondition {
	alternatives = simplifyPricingAlternatives(alternatives)
	shared := slices.Clone(alternatives[0])
	for _, alternative := range alternatives[1:] {
		shared = slices.DeleteFunc(shared, func(guard billingexpr.DisplayUsageGuard) bool {
			return !slices.ContainsFunc(alternative, func(candidate billingexpr.DisplayUsageGuard) bool {
				return pricingGuardKey(candidate) == pricingGuardKey(guard)
			})
		})
	}
	condition := &effectivePricingTierCondition{Kind: "usage_enum", Guards: shared}
	for _, guard := range shared {
		if guard.Numeric != nil {
			condition.Kind = "usage_conditions"
			break
		}
	}
	if len(alternatives) == 1 {
		if len(shared) == 0 {
			return nil
		}
		return condition
	}
	condition.Kind = "usage_alternatives"
	for _, alternative := range alternatives {
		remaining := slices.DeleteFunc(slices.Clone(alternative), func(guard billingexpr.DisplayUsageGuard) bool {
			return slices.ContainsFunc(shared, func(candidate billingexpr.DisplayUsageGuard) bool {
				return pricingGuardKey(candidate) == pricingGuardKey(guard)
			})
		})
		condition.AnyOf = append(condition.AnyOf, effectivePricingGuardSet{Guards: remaining})
	}
	return condition
}

func pricingVectorKey(tier effectivePricingTier) string {
	prices := slices.Clone(tier.UnitPrices)
	sort.Slice(prices, func(left, right int) bool {
		if prices[left].Component != prices[right].Component {
			return prices[left].Component < prices[right].Component
		}
		if prices[left].Unit != prices[right].Unit {
			return prices[left].Unit < prices[right].Unit
		}
		return prices[left].AmountRub < prices[right].AmountRub
	})
	raw, _ := common.Marshal(prices)
	return tier.Label + ":" + string(raw)
}

func compactPricingTiers(tiers []effectivePricingTier) []effectivePricingTier {
	result := make([]effectivePricingTier, 0, len(tiers))
	alternatives := make(map[int][][]billingexpr.DisplayUsageGuard)
	groups := make(map[string]int)
	for _, tier := range tiers {
		tier.UnitPrices = slices.Clone(tier.UnitPrices)
		if len(tier.UnitPrices) > 1 {
			tier.PriceComposition = "additive"
		}
		if tier.Condition != nil && tier.Condition.Kind != "usage_enum" && tier.Condition.Kind != "usage_conditions" {
			result = append(result, tier)
			continue
		}
		var guards []billingexpr.DisplayUsageGuard
		if tier.Condition != nil {
			guards = tier.Condition.Guards
		}
		normalized, possible := normalizePricingGuards(guards)
		if !possible {
			continue
		}
		key := pricingVectorKey(tier)
		index, found := groups[key]
		if !found {
			index = len(result)
			groups[key] = index
			result = append(result, tier)
		}
		alternatives[index] = append(alternatives[index], normalized)
	}
	for index, branches := range alternatives {
		result[index].Condition = pricingAlternativeCondition(branches)
	}
	return result
}

func compactOperationTariff(tariff effectiveOperationTariff) effectiveOperationTariff {
	summary := &effectivePricingSummary{Kind: "request_dependent", UnitPrices: []effectiveUnitPrice{}, IsFrom: tariff.Source == "openrouter_actual_cost"}
	tariff.Summary = summary
	if tariff.Status == "unavailable" {
		summary.Kind = "unavailable"
		return tariff
	}
	if tariff.Status != "priced" {
		return tariff
	}
	if tariff.BillingSurface == "task" {
		tariff.Tiers = compactPricingTiers(tariff.Tiers)
		if len(tariff.Tiers) == 0 {
			return tariff
		}
		if len(tariff.Tiers) != 1 || tariff.Tiers[0].Condition != nil {
			summary.Kind = "conditional"
			return tariff
		}
		summary.Kind = "unit_prices"
		if tariff.Tiers[0].PriceComposition == "additive" {
			summary.Kind = "additive"
		}
		summary.UnitPrices = slices.Clone(tariff.Tiers[0].UnitPrices)
		return tariff
	}
	if len(tariff.Tiers) == 0 {
		return tariff
	}
	chosen, amount := -1, 0.0
	unit := ""
	for index, tier := range tariff.Tiers {
		if len(tier.UnitPrices) == 0 {
			return tariff
		}
		total := 0.0
		for _, price := range tier.UnitPrices {
			if unit == "" {
				unit = price.Unit
			}
			if unit != price.Unit {
				summary.Kind = "conditional"
				return tariff
			}
			total += price.AmountRub
		}
		if chosen < 0 || total < amount {
			chosen, amount = index, total
		}
	}
	summary.Kind = "unit_prices"
	summary.UnitPrices = slices.Clone(tariff.Tiers[chosen].UnitPrices)
	summary.IsFrom = tariff.Source == "openrouter_actual_cost" || len(tariff.Tiers) > 1 || tariff.Tiers[chosen].Condition != nil
	return tariff
}
