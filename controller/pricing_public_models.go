package controller

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type publicPricingModelsPage struct {
	SchemaVersion   string               `json:"schema_version"`
	CatalogRevision string               `json:"catalog_revision"`
	Currency        string               `json:"currency"`
	Page            int                  `json:"p"`
	PageSize        int                  `json:"page_size"`
	TotalCount      int                  `json:"total_count"`
	Data            []publicPricingModel `json:"data"`
}

type publicPricingModel struct {
	CatalogType            string                   `json:"catalog_type,omitempty"`
	DisplayName            string                   `json:"display_name,omitempty"`
	ModelName              string                   `json:"model_name"`
	Description            string                   `json:"description,omitempty"`
	Icon                   string                   `json:"icon,omitempty"`
	Tags                   []string                 `json:"tags"`
	OwnerBy                string                   `json:"owner_by"`
	SupportedEndpointTypes []constant.EndpointType  `json:"supported_endpoint_types"`
	Creator                *effectivePricingCreator `json:"creator"`
	Tariff                 publicModelTariff        `json:"tariff"`
}

type publicModelTariff struct {
	Operations []publicPricingOperation `json:"operations"`
}

type publicPricingOperation struct {
	Operation string              `json:"operation"`
	Mode      string              `json:"mode"`
	Prices    []consumerUnitPrice `json:"prices"`
	Tiers     []publicPricingTier `json:"tiers"`
	IsFrom    bool                `json:"is_from"`
	IsFree    bool                `json:"is_free"`
}

type publicPricingTier struct {
	Condition *publicPricingCondition `json:"condition,omitempty"`
	Prices    []consumerUnitPrice     `json:"prices"`
}

type publicPricingCondition struct {
	Kind        string                   `json:"kind"`
	Subject     string                   `json:"subject,omitempty"`
	Argument    string                   `json:"argument,omitempty"`
	Unit        string                   `json:"unit,omitempty"`
	Operator    string                   `json:"operator,omitempty"`
	StringValue *string                  `json:"string_value,omitempty"`
	Value       *float64                 `json:"value,omitempty"`
	Conditions  []publicPricingCondition `json:"conditions,omitempty"`
}

type publicPricingModelsQuery struct {
	catalogType        string
	catalogTypeMissing bool
	expectedRevision   string
	tags               []string
	page               int
	pageSize           int
}

func pricingTags(value string) []string {
	tags := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || unicode.IsSpace(r)
	})
	slices.Sort(tags)
	return slices.Compact(tags)
}

func parsePublicPricingModelsQuery(raw string) (publicPricingModelsQuery, error) {
	query := publicPricingModelsQuery{page: 1, pageSize: 10}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return query, fmt.Errorf("invalid pricing query")
	}
	revision, err := expectedPricingRevision(values)
	if err != nil {
		return query, err
	}
	query.expectedRevision = revision
	query.catalogTypeMissing, err = parseCatalogTypeMissing(values)
	if err != nil {
		return query, err
	}
	for key, entries := range values {
		if key != "tag" && key != "p" && key != "page_size" && key != "expected_revision" && key != "catalog_type" && key != "catalog_type_missing" {
			return query, fmt.Errorf("unsupported %s", key)
		}
		if key != "tag" && len(entries) != 1 {
			return query, fmt.Errorf("invalid %s", key)
		}
	}
	if raw, present := values["catalog_type"]; present {
		if !slices.Contains(catalogsync.ModelTypes(), raw[0]) {
			return query, fmt.Errorf("invalid catalog_type")
		}
		query.catalogType = raw[0]
	}
	if len(values["tag"]) == 0 {
		return query, fmt.Errorf("tag is required")
	}
	for _, value := range values["tag"] {
		normalized := strings.ToLower(strings.TrimSpace(value))
		tags := pricingTags(normalized)
		if len(tags) != 1 || tags[0] != normalized {
			return query, fmt.Errorf("invalid tag")
		}
		query.tags = append(query.tags, normalized)
	}
	slices.Sort(query.tags)
	query.tags = slices.Compact(query.tags)
	for _, param := range []struct {
		name    string
		target  *int
		maximum int64
	}{
		{"p", &query.page, 9007199254740991}, {"page_size", &query.pageSize, 100},
	} {
		if entries, ok := values[param.name]; ok {
			if entries[0] == "" || strings.IndexFunc(entries[0], func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return query, fmt.Errorf("invalid %s", param.name)
			}
			number, err := strconv.ParseInt(entries[0], 10, 64)
			if err != nil || number < 1 || number > param.maximum {
				return query, fmt.Errorf("invalid %s", param.name)
			}
			*param.target = int(number)
		}
	}
	return query, nil
}

func GetPublicPricingModels(c *gin.Context) {
	query, err := parsePublicPricingModelsQuery(c.Request.URL.RawQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	context, err := loadConsumerPricingContext("default", "default")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "failed to resolve public pricing"})
		return
	}
	if !pricingRevisionMatches(c, query.expectedRevision, context.metadata.CatalogRevision) {
		return
	}
	context.billable = billablePricingModels(context.pricing, "default", context.metadata.CatalogRevision)
	response := buildPublicPricingModelsPage(context, query)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": response})
}

func publicTariffConfirmed(tariff publicModelTariff) bool {
	return slices.ContainsFunc(tariff.Operations, func(operation publicPricingOperation) bool {
		return confirmedConsumerPrices(operation.Prices, operation.Tiers)
	})
}

func buildPublicPricingModelsPage(context effectivePricingPageContext, query publicPricingModelsQuery) publicPricingModelsPage {
	response := publicPricingModelsPage{SchemaVersion: "v1", CatalogRevision: context.metadata.CatalogRevision, Currency: "RUB", Page: query.page, PageSize: query.pageSize, Data: []publicPricingModel{}}
	// The public catalog always projects the default tariff, independently of the user's usable groups.
	context.usableGroups = map[string]string{"default": "default"}
	pricing := model.ConsumerPricingCatalogue(context.pricing, context.usableGroups)
	type pricedModel struct {
		item   model.Pricing
		tariff publicModelTariff
	}
	candidates := make([]pricedModel, 0, len(pricing))
	for _, item := range pricing {
		if query.catalogTypeMissing && item.CatalogType != "" || query.catalogType != "" && item.CatalogType != query.catalogType {
			continue
		}
		if context.billable != nil && !context.billable[item.ModelName] {
			continue
		}
		tags := pricingTags(item.Tags)
		if slices.ContainsFunc(query.tags, func(tag string) bool { return !slices.Contains(tags, tag) }) {
			continue
		}
		// Models without a single confirmed price are not offered publicly; a zero price still counts.
		tariff := buildPublicModelTariff(context, item)
		if publicTariffConfirmed(tariff) {
			candidates = append(candidates, pricedModel{item: item, tariff: tariff})
		}
	}
	slices.SortFunc(candidates, func(a, b pricedModel) int { return compareEffectivePricingNames(a.item.ModelName, b.item.ModelName) })
	response.TotalCount = len(candidates)
	start := len(candidates)
	if query.page-1 <= len(candidates)/query.pageSize {
		start = min((query.page-1)*query.pageSize, len(candidates))
	}
	end := start + min(query.pageSize, len(candidates)-start)
	creators := make(map[int]*effectivePricingCreator, len(context.vendors))
	for _, vendor := range context.vendors {
		if name := trimPricingText(vendor.Name); vendor.ID != 0 && name != "" {
			creators[vendor.ID] = &effectivePricingCreator{ID: vendor.ID, Name: name}
		}
	}
	for _, candidate := range candidates[start:end] {
		item := candidate.item
		response.Data = append(response.Data, publicPricingModel{CatalogType: item.CatalogType, DisplayName: item.DisplayName, ModelName: item.ModelName, Description: item.Description, Icon: item.Icon, Tags: pricingTags(item.Tags), OwnerBy: item.OwnerBy, SupportedEndpointTypes: append([]constant.EndpointType{}, item.SupportedEndpointTypes...), Creator: creators[item.VendorID], Tariff: candidate.tariff})
	}
	return response
}

func publicPricingGuard(guard billingexpr.TierCondition, units ...map[string]string) publicPricingCondition {
	condition := publicPricingCondition{Kind: "comparison", Subject: guard.Var, Argument: guard.Arg, Operator: guard.Op, Value: &guard.Value}
	if guard.Var == "len" {
		condition.Subject = "context_length"
		condition.Unit = "token"
	} else if componentName(guard.Var) != guard.Var || guard.Var == "p" || guard.Var == "c" {
		condition.Subject = componentName(guard.Var)
		if len(units) > 0 {
			condition.Unit = units[0][condition.Subject]
		}
	}
	if guard.Op == "&&" || guard.Op == "||" {
		condition = publicPricingCondition{Kind: "all_of"}
		if guard.Op == "||" {
			condition.Kind = "any_of"
		}
		for _, operand := range guard.Operands {
			condition.Conditions = append(condition.Conditions, publicPricingGuard(operand, units...))
		}
	}
	if guard.Negated {
		condition = publicPricingCondition{Kind: "not", Conditions: []publicPricingCondition{condition}}
	}
	return condition
}

func buildPublicModelTariff(context effectivePricingPageContext, item model.Pricing) publicModelTariff {
	return buildCatalogModelTariff(context, item, "default")
}

func buildCatalogModelTariff(context effectivePricingPageContext, item model.Pricing, group string) publicModelTariff {
	result := publicModelTariff{Operations: []publicPricingOperation{}}
	offers := item.PublicCatalogOffersByGroup[group]
	if len(offers) == 0 {
		offers = item.CatalogOffersForGroup(group)
	}
	byOperation := make(map[string][]model.CatalogOffer)
	for _, offer := range offers {
		key := offer.Operation + ":" + offer.Mode
		byOperation[key] = append(byOperation[key], offer)
	}
	keys := make([]string, 0, len(byOperation))
	for key := range byOperation {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	ratio, ratioErr := service.ApplyBillingFX(context.groupRatios[group], context.fxFactor)
	for _, key := range keys {
		operation, mode, _ := strings.Cut(key, ":")
		row := publicPricingOperation{Operation: operation, Mode: mode, Prices: []consumerUnitPrice{}, Tiers: []publicPricingTier{}, IsFree: true}
		candidates := byOperation[key]
		selected := model.SelectCatalogOffer(candidates)
		sources := item.BillingSourcesForGroup(group)
		actualCost := false
		for _, values := range sources {
			actualCost = actualCost || slices.Contains(values, model.BillingSourceOpenRouterActualCost)
		}
		row.IsFrom = actualCost || len(candidates) > 1
		unknown := ratioErr != nil
		seen := make(map[string]bool)
		for _, offer := range candidates {
			tier := publicPricingTier{Prices: []consumerUnitPrice{}}
			if len(offer.PublicConditions) > 0 || len(offer.PublicUsageConditions) > 0 {
				condition := publicPricingCondition{Kind: "all_of"}
				units := make(map[string]string)
				for _, price := range offer.Prices {
					if price.Unit == "million_tokens" {
						units[price.Component] = "token"
					} else if price.Unit == "million_characters" {
						units[price.Component] = "character"
					}
				}
				for _, guard := range offer.PublicConditions {
					condition.Conditions = append(condition.Conditions, publicPricingGuard(guard, units))
				}
				for _, guard := range offer.PublicUsageConditions {
					comparison := publicPricingCondition{Kind: "comparison", Subject: "usage", Argument: guard.Field, Operator: "==", StringValue: &guard.Value}
					if guard.Negated {
						comparison = publicPricingCondition{Kind: "not", Conditions: []publicPricingCondition{comparison}}
					}
					condition.Conditions = append(condition.Conditions, comparison)
				}
				if len(condition.Conditions) == 1 {
					condition = condition.Conditions[0]
				}
				tier.Condition = &condition
			}
			row.IsFrom = row.IsFrom || offer.PublicConditional || tier.Condition != nil
			if len(offer.Prices) == 0 {
				unknown = true
			}
			for _, price := range offer.Prices {
				public := consumerUnitPrice{Component: price.Component, Unit: price.Unit}
				if ratioErr == nil && price.AmountUSD != nil && price.Unit != "" && price.Unit != "unknown" {
					amount := *price.AmountUSD * 100 * ratio
					if amount >= 0 && !math.IsNaN(amount) && !math.IsInf(amount, 0) {
						public.AmountRub = &amount
					}
				}
				if public.AmountRub == nil {
					unknown = true
				} else if *public.AmountRub != 0 {
					row.IsFree = false
				}
				tier.Prices = append(tier.Prices, public)
			}
			encoded, err := common.Marshal(tier)
			if err == nil && !seen[string(encoded)] {
				row.Tiers = append(row.Tiers, tier)
				seen[string(encoded)] = true
			}
			if selected != nil && selected.ID == offer.ID {
				row.Prices = tier.Prices
			}
		}
		row.IsFree = row.IsFree && !unknown
		result.Operations = append(result.Operations, row)
	}
	minima := catalogMinimumOperations(iterateUpstreamProviderOffers(item, group, ratio, ratioErr == nil, pricingUserContext{}))
	for _, operation := range minima {
		index := slices.IndexFunc(result.Operations, func(row publicPricingOperation) bool {
			return row.Operation == operation.Operation && row.Mode == operation.Mode
		})
		if index < 0 {
			result.Operations = append(result.Operations, publicPricingOperation{Operation: operation.Operation, Mode: operation.Mode, Prices: []consumerUnitPrice{}, Tiers: []publicPricingTier{}})
			index = len(result.Operations) - 1
		}
		row := &result.Operations[index]
		row.Tiers = append(row.Tiers, operation.Tiers...)
		merged := mergeCatalogMinima([]consumerPricingOperation{{Operation: row.Operation, Mode: row.Mode, Prices: row.Prices}}, []consumerPricingOperation{operation})
		row.Prices = merged[0].Prices
		row.IsFrom = row.IsFrom || *merged[0].IsFrom
		// Independent component minima do not prove that a complete request is free.
		row.IsFree = false
	}
	result.Operations = slices.DeleteFunc(result.Operations, func(operation publicPricingOperation) bool {
		return item.CatalogModality != "" && operation.Mode == "batch" && !confirmedConsumerPrices(operation.Prices, operation.Tiers)
	})
	return result
}
