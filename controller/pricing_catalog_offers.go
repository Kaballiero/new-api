package controller

import (
	"encoding/json"
	"iter"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/provider"
	"github.com/shopspring/decimal"
)

type upstreamRate catalogsync.Component

type consumerProviderPrice struct {
	upstreamRate
	Component string  `json:"component"`
	AmountRub *string `json:"amount_rub"`
}
type consumerProviderOffer struct {
	Availability  string                   `json:"catalog_availability"`
	Limitations   []string                 `json:"limitations"`
	ID            string                   `json:"id"`
	UpstreamModel string                   `json:"upstream_model"`
	Operation     string                   `json:"operation"`
	Mode          string                   `json:"mode"`
	Scope         string                   `json:"scope"`
	Provider      *catalogsync.Provider    `json:"provider"`
	Variant       json.RawMessage          `json:"variant"`
	Prices        []consumerProviderPrice  `json:"prices"`
	Coverage      catalogsync.Coverage     `json:"coverage"`
	Eligibility   *catalogsync.Eligibility `json:"eligibility"`
	Capabilities  json.RawMessage          `json:"capabilities"`
	Limits        json.RawMessage          `json:"limits"`
}

type pricingUserContext struct {
	id    int
	group string
}

func catalogComponentName(parameter string) string {
	switch parameter {
	case "prompt":
		return "input"
	case "completion":
		return "output"
	case "input_cache_read", "cached_input":
		return "cache_read"
	case "input_cache_write":
		return "cache_write"
	case "input_cache_write_1h":
		return "cache_write_1h"
	case "request":
		return "base"
	default:
		return parameter
	}
}

// A catalog is a projection, not request admission. Use the same override engine
// as relay; apply provider selection to the resulting routing preferences.
func catalogProviderAllowed(route model.UpstreamCatalogRoute, apiModel, group string, user pricingUserContext, offer catalogsync.Offer) (bool, bool) {
	if route.ParamOverride == nil || strings.TrimSpace(*route.ParamOverride) == "" || strings.TrimSpace(*route.ParamOverride) == "{}" {
		return true, true
	}
	var overrides map[string]any
	if common.UnmarshalJsonStr(*route.ParamOverride, &overrides) != nil {
		return false, true
	}
	// Headers, payload parameters, token selection and retries are absent from a
	// catalog request. Do not evaluate their conditional branches as known false.
	if operations, exists := overrides["operations"]; exists {
		entries, ok := operations.([]any)
		if !ok {
			return true, false
		}
		for _, entry := range entries {
			operation, ok := entry.(map[string]any)
			if !ok {
				return true, false
			}
			if operation["mode"] != "set" && operation["mode"] != "delete" {
				return true, false
			}
			paths := []string{}
			switch conditions := operation["conditions"].(type) {
			case nil:
			case map[string]any:
				for path := range conditions {
					paths = append(paths, path)
				}
			case []any:
				for _, raw := range conditions {
					condition, ok := raw.(map[string]any)
					if !ok {
						return true, false
					}
					path, _ := condition["path"].(string)
					paths = append(paths, path)
				}
			default:
				return true, false
			}
			for _, path := range paths {
				switch strings.TrimSpace(path) {
				case "user_id":
					if user.id <= 0 {
						return true, false
					}
				case "user_group":
					if user.group == "" {
						return true, false
					}
				case "using_group", "model", "upstream_model", "original_model", "request_path":
				default:
					return true, false
				}
			}
		}
	}

	body, err := common.Marshal(map[string]string{"model": route.ModelID})
	if err != nil {
		return false, true
	}
	path, _ := common.GetDefaultEndpointInfo(route.Endpoint)
	info := &relaycommon.RelayInfo{UserId: user.id, UserGroup: user.group, TokenGroup: user.group, UsingGroup: group, OriginModelName: apiModel, RequestURLPath: path.Path, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: route.ModelID, ParamOverride: overrides}}
	body, err = relaycommon.ApplyParamOverride(body, overrides, relaycommon.BuildParamOverrideContext(info))
	if err != nil {
		return false, true
	}
	body, err = provider.Normalize(body)
	if err != nil {
		return false, true
	}
	var fields struct {
		Provider struct {
			Only, Ignore, Order []string
			AllowFallbacks      *bool `json:"allow_fallbacks"`
		} `json:"provider"`
	}
	if common.Unmarshal(body, &fields) != nil {
		return false, true
	}
	var routing struct {
		Model    string                     `json:"model"`
		Provider map[string]json.RawMessage `json:"provider"`
	}
	if common.Unmarshal(body, &routing) != nil {
		return false, true
	}
	certain := routing.Model == route.ModelID
	for key, value := range routing.Provider {
		switch key {
		case "only", "ignore", "order", "allow_fallbacks", "sort":
		default:
			if string(value) != "null" {
				certain = false
			}
		}
	}
	preferences := fields.Provider
	if len(preferences.Only) == 0 && len(preferences.Ignore) == 0 && (preferences.AllowFallbacks == nil || *preferences.AllowFallbacks || len(preferences.Order) == 0) {
		return true, certain
	}
	if offer.Provider == nil {
		return false, true
	}
	tag := ""
	if offer.Provider.Tag != nil {
		tag = *offer.Provider.Tag
	} else if offer.Provider.ID != nil {
		tag = *offer.Provider.ID
	}
	matches := func(slug string) bool {
		candidate := strings.ToLower(tag)
		if base, ok := strings.CutSuffix(candidate, "/priority"); ok {
			candidate = base + "/fast"
		}
		slug = strings.ToLower(slug)
		return candidate == slug || strings.HasPrefix(candidate, slug+"/")
	}
	if len(preferences.Only) > 0 && !slices.ContainsFunc(preferences.Only, matches) || slices.ContainsFunc(preferences.Ignore, matches) {
		return false, true
	}
	return preferences.AllowFallbacks == nil || *preferences.AllowFallbacks || len(preferences.Order) == 0 || slices.ContainsFunc(preferences.Order, matches), certain
}

func upstreamProviderOffers(item model.Pricing, group string, ratio float64, validRatio bool, user pricingUserContext, public bool) []consumerProviderOffer {
	result := []consumerProviderOffer{}
	seen := map[string]int{}
	for offer := range iterateUpstreamProviderOffers(item, group, ratio, validRatio, user, public) {
		if previous, exists := seen[offer.ID]; exists {
			if result[previous].Availability == "unknown" && offer.Availability == "eligible" {
				result[previous] = offer
			}
			continue
		}
		seen[offer.ID] = len(result)
		result = append(result, offer)
	}
	slices.SortFunc(result, func(a, b consumerProviderOffer) int { return strings.Compare(a.ID, b.ID) })
	return result
}

func iterateUpstreamProviderOffers(item model.Pricing, group string, ratio float64, validRatio bool, user pricingUserContext, public bool) iter.Seq[consumerProviderOffer] {
	return func(yield func(consumerProviderOffer) bool) {
		for _, route := range item.UpstreamCatalogRoutesForGroup(group) {
			if route.Document == nil {
				continue
			}
			for offerIndex, offer := range route.Document.Offers {
				if offer.Operation != nil && *offer.Operation != route.Operation || offer.Mode != nil && *offer.Mode != route.Mode || offer.Mode == nil && route.Mode != "sync" {
					continue
				}
				certain := true
				if !public {
					allowed, known := catalogProviderAllowed(route, item.ModelName, group, user, offer)
					if !allowed {
						continue
					}
					certain = known && offer.Eligibility != nil
				}
				id := catalogsync.Hash([]byte(route.DocumentHash + "\n" + route.ModelID + "\n" + strconv.Itoa(offerIndex) + "\n" + route.Operation + "\n" + route.Mode))
				row := consumerProviderOffer{ID: id, UpstreamModel: route.ModelID, Operation: route.Operation, Mode: route.Mode, Scope: offer.Scope, Provider: offer.Provider, Variant: offer.Variant, Prices: []consumerProviderPrice{}, Coverage: offer.Coverage, Eligibility: offer.Eligibility, Capabilities: offer.Capabilities, Limits: offer.Limits}
				row.Limitations = slices.Clone(offer.Limitations)
				row.Availability = "eligible"
				if offer.Eligibility != nil && !offer.Eligibility.Eligible {
					row.Availability = "ineligible"
				}
				if !certain {
					row.Availability = "unknown"
					row.Limitations = append(row.Limitations, "request_dependent_provider_eligibility")
				}
				for _, rate := range offer.Components {
					price := consumerProviderPrice{upstreamRate: upstreamRate(rate), Component: catalogComponentName(rate.Parameter)}
					if validRatio && ratio >= 0 && !math.IsNaN(ratio) && !math.IsInf(ratio, 0) && rate.AmountUSD != nil && rate.Normalization.Comparable && rate.Normalization.Effective && rate.Unit != "unknown" && rate.Unit != "source_defined" {
						usd, parseErr := decimal.NewFromString(*rate.AmountUSD)
						if parseErr == nil && !usd.IsNegative() {
							rub := usd.Mul(decimal.NewFromInt(100)).Mul(decimal.NewFromFloat(ratio)).String()
							price.AmountRub = &rub
						}
					}
					row.Prices = append(row.Prices, price)
				}
				if !yield(row) {
					return
				}
			}
		}
	}
}

func catalogMinima(offers []consumerProviderOffer) []consumerPricingOperation {
	return catalogMinimumOperations(slices.Values(offers))
}

func catalogMinimumOperations(offers iter.Seq[consumerProviderOffer]) []consumerPricingOperation {
	type key struct{ operation, mode string }
	type winner struct {
		price           consumerProviderPrice
		value, quantity decimal.Decimal
	}
	grouped := map[key]map[string]winner{}
	for offer := range offers {
		if offer.Availability == "unknown" || offer.Eligibility != nil && !offer.Eligibility.Eligible {
			continue
		}
		k := key{offer.Operation, offer.Mode}
		if grouped[k] == nil {
			grouped[k] = map[string]winner{}
		}
		for _, price := range offer.Prices {
			if price.AmountRub == nil {
				continue
			}
			value, err := decimal.NewFromString(*price.AmountRub)
			if err != nil || value.IsNegative() {
				continue
			}
			quantity, err := decimal.NewFromString(price.Quantity)
			if err != nil || !quantity.IsPositive() {
				continue
			}
			slot := price.Component + "/" + price.Unit
			old, exists := grouped[k][slot]
			if !exists || value.Mul(old.quantity).LessThan(old.value.Mul(quantity)) {
				grouped[k][slot] = winner{price, value, quantity}
			}
		}
	}
	keys := make([]key, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b key) int {
		if a.operation != b.operation {
			return strings.Compare(a.operation, b.operation)
		}
		return strings.Compare(a.mode, b.mode)
	})
	result := []consumerPricingOperation{}
	for _, k := range keys {
		row := consumerPricingOperation{Operation: k.operation, Mode: k.mode, Prices: []consumerUnitPrice{}}
		from := true
		row.IsFrom = &from
		for _, selected := range grouped[k] {
			precision := max(int32(32), -selected.value.Exponent()+int32(selected.quantity.NumDigits())+32)
			value := selected.value.DivRound(selected.quantity, precision)
			converted, _ := value.Float64()
			if math.IsNaN(converted) || math.IsInf(converted, 0) || converted == 0 && !selected.value.IsZero() {
				continue
			}
			amount := converted
			row.Prices = append(row.Prices, consumerUnitPrice{Component: selected.price.Component, Unit: selected.price.Unit, AmountRub: &amount})
		}
		slices.SortFunc(row.Prices, func(a, b consumerUnitPrice) int {
			if a.Component != b.Component {
				return strings.Compare(a.Component, b.Component)
			}
			return strings.Compare(a.Unit, b.Unit)
		})
		result = append(result, row)
	}
	return result
}

func mergeCatalogMinima(existing, upstream []consumerPricingOperation) []consumerPricingOperation {
	for _, addition := range upstream {
		index := slices.IndexFunc(existing, func(row consumerPricingOperation) bool {
			return row.Operation == addition.Operation && row.Mode == addition.Mode
		})
		if index < 0 {
			existing = append(existing, addition)
			continue
		}
		row := &existing[index]
		from := true
		row.IsFrom = &from
		row.Prices = slices.Clone(row.Prices)
		for _, price := range addition.Prices {
			slot := slices.IndexFunc(row.Prices, func(old consumerUnitPrice) bool { return old.Component == price.Component && old.Unit == price.Unit })
			if slot < 0 {
				row.Prices = append(row.Prices, price)
			} else if row.Prices[slot].AmountRub == nil || price.AmountRub != nil && *price.AmountRub < *row.Prices[slot].AmountRub {
				row.Prices[slot] = price
			}
		}
	}
	slices.SortStableFunc(existing, func(a, b consumerPricingOperation) int {
		if a.Operation != b.Operation {
			return strings.Compare(a.Operation, b.Operation)
		}
		return strings.Compare(a.Mode, b.Mode)
	})
	return existing
}
