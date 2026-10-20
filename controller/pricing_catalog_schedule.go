package controller

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
)

// These fields describe the published endpoint schedule, not a request SKU.
type catalogPriceWindow struct {
	ContextMin *int64 `json:"contextMin"`
	ContextMax *int64 `json:"contextMax"`
	Day        *int   `json:"utcDay"`
	Start      *int   `json:"utcStart"`
	End        *int   `json:"utcEnd"`
}

type catalogScheduledPrice struct {
	price           consumerProviderPrice
	tier            string
	low, high       int64
	day, start, end int
}

const catalogUnboundedContext int64 = 9007199254740991
const catalogMaxScheduleCells = 4096

func catalogServiceTier(variant []byte) string {
	var fields struct {
		Tier string `json:"service_tier"`
	}
	if len(variant) != 0 && common.Unmarshal(variant, &fields) != nil {
		return "unknown"
	}
	if fields.Tier == "" || fields.Tier == "standard" {
		return "default"
	}
	if fields.Tier == "fast" {
		return "priority"
	}
	return fields.Tier
}

func catalogServiceTierAllowed(route model.UpstreamCatalogRoute, offer catalogsync.Offer, requested string, only, order []string) bool {
	tier := catalogServiceTier(offer.Variant)
	if tier == "unknown" {
		return false
	}
	if tier == "default" {
		return true
	}
	if offer.Provider != nil && offer.Provider.Tag != nil {
		tag := *offer.Provider.Tag
		if slices.Contains(only, tag) || slices.Contains(order, tag) {
			return true
		}
	}
	if requested == "fast" {
		requested = "priority"
	}
	if requested != "" {
		return tier == requested
	}
	if len(order) == 0 && (tier == "flex" && strings.HasSuffix(route.ModelID, ":floor") || tier == "priority" && strings.HasSuffix(route.ModelID, ":nitro")) {
		return true
	}
	// Only expose opt-in prices in a minimum when the channel preserves that
	// option. Explicit admin overrides above are applied after field removal.
	return (tier == "flex" || tier == "priority" || tier == "ultrafast") && route.ServiceTierEnabled
}

func catalogSchedulePrices(offer consumerProviderOffer) []consumerProviderPrice {
	generic := map[string]bool{}
	for _, price := range offer.Prices {
		var provenance struct {
			Source string `json:"source"`
		}
		if common.Unmarshal(price.Provenance, &provenance) == nil && provenance.Source == "generic" {
			generic[price.Component+"/"+price.Unit] = true
		}
	}
	result := []consumerProviderPrice{}
	for _, price := range offer.Prices {
		if price.Component == "unknown" || price.AmountRub == nil {
			continue
		}
		var provenance struct {
			Source string `json:"source"`
		}
		if common.Unmarshal(price.Provenance, &provenance) != nil {
			continue
		}
		// A native model-page meter can lack the endpoint's context schedule.
		// Keep it in details; it cannot replace that schedule unconditionally.
		if provenance.Source == "model_page" && generic[price.Component+"/"+price.Unit] {
			continue
		}
		if _, valid := catalogRateWindow(price); !valid {
			continue
		}
		result = append(result, price)
	}
	return result
}

func catalogRateWindow(price consumerProviderPrice) (catalogPriceWindow, bool) {
	window := catalogPriceWindow{}
	if len(price.Conditions) == 0 || string(price.Conditions) == "null" {
		return window, true
	}
	var fields map[string]any
	if common.Unmarshal(price.Conditions, &fields) != nil || fields == nil {
		return window, false
	}
	for key := range fields {
		switch key {
		case "contextMin", "contextMax", "utcDay", "utcStart", "utcEnd", "appliedOverrideIndexes":
		default:
			return window, false
		}
	}
	if common.Unmarshal(price.Conditions, &window) != nil {
		return window, false
	}
	if window.ContextMin != nil && (*window.ContextMin < 0 || *window.ContextMin > catalogUnboundedContext) || window.ContextMax != nil && (*window.ContextMax < 0 || *window.ContextMax > catalogUnboundedContext) {
		return window, false
	}
	if window.ContextMin != nil && window.ContextMax != nil && *window.ContextMax < *window.ContextMin || window.Day != nil && (*window.Day < 0 || *window.Day > 6) {
		return window, false
	}
	start, end := 0, 2400
	if window.Start != nil {
		start = *window.Start
	}
	if window.End != nil {
		end = *window.End
	}
	return window, start >= 0 && start < end && end <= 2400 && start%100 < 60 && end%100 < 60
}

func catalogScheduleTiers(rates []catalogScheduledPrice) []publicPricingTier {
	byTier := map[string][]catalogScheduledPrice{}
	for _, rate := range rates {
		byTier[rate.tier] = append(byTier[rate.tier], rate)
	}
	tiers := []publicPricingTier{}
	serviceTiers := make([]string, 0, len(byTier))
	for tier := range byTier {
		serviceTiers = append(serviceTiers, tier)
	}
	slices.Sort(serviceTiers)
	for _, tier := range serviceTiers {
		entries := byTier[tier]
		contexts, times := []int64{}, []int{}
		for _, rate := range entries {
			contexts = append(contexts, rate.low, rate.high+1)
			times = append(times, rate.start, rate.end)
		}
		slices.Sort(contexts)
		contexts = slices.Compact(contexts)
		slices.Sort(times)
		times = slices.Compact(times)
		// A pathological snapshot must not multiply a list response without a
		// bound. Unknown schedules are never rendered as an unconditional tier.
		if len(contexts)*len(times)*7 > catalogMaxScheduleCells {
			return nil
		}
		for contextIndex := 0; contextIndex+1 < len(contexts); contextIndex++ {
			low, high := contexts[contextIndex], contexts[contextIndex+1]-1
			for timeIndex := 0; timeIndex+1 < len(times); timeIndex++ {
				start, end := times[timeIndex], times[timeIndex+1]
				byPrices := map[string][]int{}
				pricesByKey := map[string][]consumerUnitPrice{}
				for day := range 7 {
					candidates := consumerProviderOffer{Availability: "eligible", minimumEligible: true, Prices: []consumerProviderPrice{}}
					for _, rate := range entries {
						if rate.low <= low && rate.high >= high && rate.start <= start && rate.end >= end && (rate.day == -1 || rate.day == day) {
							candidates.Prices = append(candidates.Prices, rate.price)
						}
					}
					// The inner reduction only computes component minima; conditions
					// have already been applied to this exact schedule cell.
					minimum := catalogUnconditionalMinima(slices.Values([]consumerProviderOffer{candidates}))
					if len(minimum) == 0 || len(minimum[0].Prices) == 0 {
						continue
					}
					prices := minimum[0].Prices
					wire, err := common.Marshal(prices)
					if err != nil {
						continue
					}
					key := string(wire)
					byPrices[key] = append(byPrices[key], day)
					pricesByKey[key] = prices
				}
				keys := make([]string, 0, len(byPrices))
				for key := range byPrices {
					keys = append(keys, key)
				}
				slices.Sort(keys)
				for _, key := range keys {
					conditions := []publicPricingCondition{}
					if tier != "default" || len(serviceTiers) > 1 {
						value := tier
						conditions = append(conditions, publicPricingCondition{Kind: "comparison", Subject: "request", Argument: "service_tier", Operator: "==", StringValue: &value})
					}
					if low > 0 {
						value := float64(low)
						conditions = append(conditions, publicPricingCondition{Kind: "comparison", Subject: "context", Argument: "tokens", Unit: "token", Operator: ">=", Value: &value})
					}
					if high != catalogUnboundedContext {
						value := float64(high)
						conditions = append(conditions, publicPricingCondition{Kind: "comparison", Subject: "context", Argument: "tokens", Unit: "token", Operator: "<=", Value: &value})
					}
					if start != 0 {
						value := float64(start)
						conditions = append(conditions, publicPricingCondition{Kind: "comparison", Subject: "time", Argument: "utc_hhmm", Operator: ">=", Value: &value})
					}
					if end != 2400 {
						value := float64(end)
						conditions = append(conditions, publicPricingCondition{Kind: "comparison", Subject: "time", Argument: "utc_hhmm", Operator: "<", Value: &value})
					}
					if len(byPrices[key]) != 7 {
						days := publicPricingCondition{Kind: "any_of"}
						for _, day := range byPrices[key] {
							value := float64(day)
							days.Conditions = append(days.Conditions, publicPricingCondition{Kind: "comparison", Subject: "time", Argument: "utc_day", Operator: "==", Value: &value})
						}
						conditions = append(conditions, days)
					}
					row := publicPricingTier{Prices: pricesByKey[key]}
					if len(conditions) == 1 {
						row.Condition = &conditions[0]
					} else if len(conditions) > 1 {
						row.Condition = &publicPricingCondition{Kind: "all_of", Conditions: conditions}
					}
					tiers = append(tiers, row)
				}
			}
		}
	}
	return tiers
}

func catalogScheduledRates(offer consumerProviderOffer) []catalogScheduledPrice {
	result := []catalogScheduledPrice{}
	for _, price := range catalogSchedulePrices(offer) {
		window, _ := catalogRateWindow(price)
		rate := catalogScheduledPrice{price: price, tier: catalogServiceTier(offer.Variant), high: catalogUnboundedContext, day: -1, end: 2400}
		if window.ContextMin != nil {
			rate.low = *window.ContextMin
		}
		if window.ContextMax != nil {
			rate.high = *window.ContextMax
		}
		if window.Day != nil {
			rate.day = *window.Day
		}
		if window.Start != nil {
			rate.start = *window.Start
		}
		if window.End != nil {
			rate.end = *window.End
		}
		result = append(result, rate)
	}
	return result
}
