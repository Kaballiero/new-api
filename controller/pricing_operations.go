package controller

import (
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

type effectiveOperationTariff struct {
	EndpointType   constant.EndpointType  `json:"endpoint_type"`
	Source         string                 `json:"source"`
	BillingSurface string                 `json:"billing_surface"`
	Status         string                 `json:"status"`
	Tiers          []effectivePricingTier `json:"tiers"`
	Limitations    []string               `json:"limitations,omitempty"`
}

func withOperationTariffs(response effectivePricingResponse, pricing []model.Pricing) effectivePricingResponse {
	items := make(map[string]model.Pricing, len(pricing))
	for _, item := range pricing {
		items[item.ModelName] = item
	}
	response.Data = slices.Clone(response.Data)
	for index := range response.Data {
		item, found := items[response.Data[index].ModelName]
		response.Data[index].GroupPrices = slices.Clone(response.Data[index].GroupPrices)
		for groupIndex := range response.Data[index].GroupPrices {
			group := &response.Data[index].GroupPrices[groupIndex]
			var tariffs []effectiveOperationTariff
			if found {
				tariffs = operationTariffs(item, *group)
			}
			group.OperationTariffs = &tariffs
		}
	}
	return response
}

func operationTariffs(item model.Pricing, group effectiveGroupPricing) []effectiveOperationTariff {
	sourcesByEndpoint := item.BillingSourcesForGroup(group.UsingGroup)
	if len(sourcesByEndpoint) == 0 {
		return nil
	}
	endpoints := make([]constant.EndpointType, 0, len(sourcesByEndpoint))
	for endpoint := range sourcesByEndpoint {
		endpoints = append(endpoints, endpoint)
	}
	sort.Slice(endpoints, func(left, right int) bool { return endpoints[left] < endpoints[right] })
	tariffs := make([]effectiveOperationTariff, 0)
	for _, endpoint := range endpoints {
		for _, source := range sourcesByEndpoint[endpoint] {
			if source != model.BillingSourceConfiguredTariff && source != model.BillingSourceOpenRouterActualCost {
				continue
			}
			tariffs = append(tariffs, buildOperationTariff(item, group, endpoint, source))
		}
	}
	return tariffs
}

func buildOperationTariff(item model.Pricing, group effectiveGroupPricing, endpoint constant.EndpointType, source string) effectiveOperationTariff {
	tariff := effectiveOperationTariff{
		EndpointType: endpoint, Source: source, BillingSurface: group.BillingSurface,
		Status: "request_dependent", Tiers: []effectivePricingTier{},
	}
	if group.Status == "unavailable" || group.EffectiveBillingRatio < 0 || math.IsNaN(group.EffectiveBillingRatio) || math.IsInf(group.EffectiveBillingRatio, 0) {
		tariff.Status, tariff.Limitations = "unavailable", []string{"invalid_group_ratio"}
		return tariff
	}
	expression := strings.TrimSpace(item.BillingExpr)
	useExpression := item.BillingMode == billing_setting.BillingModeTieredExpr
	if source == model.BillingSourceOpenRouterActualCost {
		expression = strings.TrimSpace(item.IndicativeBillingExprByEndpoint[endpoint])
		useExpression = true
	}
	if endpoint == model.PricingEndpointBatch {
		tariff.BillingSurface = "token"
		if source == model.BillingSourceConfiguredTariff {
			expression, _ = billing_setting.GetBatchBillingExpr(item.ModelName)
		}
		useExpression = true
	}
	if !useExpression {
		for _, tier := range group.Tiers {
			for _, price := range tier.UnitPrices {
				if math.IsNaN(price.AmountRub) || math.IsInf(price.AmountRub, 0) || price.AmountRub < 0 {
					tariff.Status, tariff.Limitations = "unavailable", []string{"invalid_display_price"}
					return tariff
				}
			}
		}
		tariff.Status, tariff.Tiers, tariff.Limitations = "priced", group.Tiers, group.Limitations
		return tariff
	}
	if expression == "" {
		tariff.Limitations = []string{"missing_display_expression"}
		return tariff
	}
	var schema map[string]billingexpr.DisplayUsageField
	if len(item.BillingUsageSchema) > 0 && endpoint != model.PricingEndpointBatch {
		tariff.BillingSurface = "task"
		schema = make(map[string]billingexpr.DisplayUsageField, len(item.BillingUsageSchema))
		for key, field := range item.BillingUsageSchema {
			schema[key] = billingexpr.DisplayUsageField{Unit: field.Unit, Enum: field.Enum}
		}
	}
	tiers, err := billingexpr.EnumerateDisplayTiers(expression, schema)
	if err != nil {
		tariff.Limitations = []string{"expression_not_projected"}
		return tariff
	}
	result := make([]effectivePricingTier, 0, len(tiers))
	fixedOnly := schema == nil
	for _, tier := range tiers {
		condition := classifyTierConditions(tier.Conditions)
		if len(tier.UsageGuards) > 0 {
			condition = &effectivePricingTierCondition{Kind: "usage_enum", Guards: tier.UsageGuards}
		}
		if condition != nil && condition.Kind == "other" {
			tariff.Limitations = []string{"condition_not_projected"}
			return tariff
		}
		prices := make([]effectiveUnitPrice, 0, len(tier.Prices))
		for _, price := range tier.Prices {
			if price.Unit != "call" {
				fixedOnly = false
			}
			amount := price.AmountUSD * group.EffectiveBillingRatio * 100
			if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
				tariff.Status, tariff.Limitations = "unavailable", []string{"invalid_display_price"}
				return tariff
			}
			component := price.Component
			if schema == nil {
				component = componentName(component)
			}
			prices = append(prices, effectiveUnitPrice{Component: component, Unit: price.Unit, AmountRub: amount})
		}
		result = append(result, effectivePricingTier{Label: tier.Label, Condition: condition, UnitPrices: prices})
	}
	if fixedOnly {
		tariff.BillingSurface = "per_call"
	}
	tariff.Status, tariff.Tiers = "priced", result
	return tariff
}
