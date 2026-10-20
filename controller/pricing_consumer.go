package controller

import (
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type consumerPricingResponse struct {
	SchemaVersion   string                 `json:"schema_version"`
	CatalogRevision string                 `json:"catalog_revision"`
	UserGroup       string                 `json:"user_group"`
	Currency        string                 `json:"currency"`
	AutoGroups      []string               `json:"auto_groups"`
	Data            []consumerPricingModel `json:"data"`
}

type consumerPricingModel struct {
	CatalogType            string                  `json:"catalog_type,omitempty"`
	DisplayName            string                  `json:"display_name,omitempty"`
	ModelName              string                  `json:"model_name"`
	Description            string                  `json:"description,omitempty"`
	Icon                   string                  `json:"icon,omitempty"`
	Tags                   string                  `json:"tags,omitempty"`
	VendorID               int                     `json:"vendor_id,omitempty"`
	OwnerBy                string                  `json:"owner_by"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	GroupPrices            []consumerGroupPricing  `json:"group_prices"`
}

type consumerGroupPricing struct {
	ProviderOffers []consumerProviderOffer    `json:"provider_offers,omitempty"`
	UsingGroup     string                     `json:"using_group"`
	Operations     []consumerPricingOperation `json:"operations"`
}

type consumerPricingOperation struct {
	Tiers     []publicPricingTier `json:"tiers,omitempty"`
	IsFrom    *bool               `json:"is_from,omitempty"`
	Operation string              `json:"operation"`
	Mode      string              `json:"mode"`
	Prices    []consumerUnitPrice `json:"prices"`
}

type consumerUnitPrice struct {
	Component string   `json:"component"`
	Unit      string   `json:"unit"`
	AmountRub *float64 `json:"amount_rub"`
}

func buildConsumerPricingResponse(userGroup string, basis service.BillingFXBasis, pricing []model.Pricing) consumerPricingResponse {
	usableGroups := service.GetUserUsableGroups(userGroup)
	groupRatios := make(map[string]float64, len(usableGroups))
	for group := range usableGroups {
		groupRatios[group] = service.GetUserGroupRatio(userGroup, group)
	}
	response := consumerPricingResponse{SchemaVersion: "v2", UserGroup: userGroup, Currency: "RUB", AutoGroups: service.GetUserAutoGroup(userGroup), Data: make([]consumerPricingModel, 0, len(pricing))}
	for _, item := range filterPricingByUsableGroups(model.ConsumerPricingCatalogue(pricing, usableGroups), usableGroups) {
		response.Data = append(response.Data, buildConsumerPricingModel(usableGroups, groupRatios, basis.Factor, item, false, pricingUserContext{group: userGroup}))
	}
	return response
}

func buildConsumerPricingModel(usableGroups map[string]string, groupRatios map[string]float64, fxFactor float64, item model.Pricing, details bool, users ...pricingUserContext) consumerPricingModel {
	result := consumerPricingModel{CatalogType: item.CatalogType, DisplayName: item.DisplayName, ModelName: item.ModelName, Description: item.Description, Icon: item.Icon, Tags: item.Tags, VendorID: item.VendorID, OwnerBy: item.OwnerBy, SupportedEndpointTypes: item.SupportedEndpointTypes, GroupPrices: []consumerGroupPricing{}}
	groups := effectivePricingGroups(item.EnableGroup, usableGroups)
	slices.Sort(groups)
	for _, group := range groups {
		ratio, err := service.ApplyBillingFX(groupRatios[group], fxFactor)
		user := pricingUserContext{}
		if len(users) > 0 {
			user = users[0]
		}
		var providers []consumerProviderOffer
		var minima []consumerPricingOperation
		if details {
			providers = upstreamProviderOffers(item, group, ratio, err == nil, user)
			minima = catalogMinima(providers)
		} else {
			minima = catalogMinimumOperations(iterateUpstreamProviderOffers(item, group, ratio, err == nil, user))
		}
		operations := mergeCatalogMinima(consumerOperations(item, group, ratio, err == nil), minima)
		if slices.ContainsFunc(item.PublicCatalogOffersByGroup[group], func(offer model.CatalogOffer) bool { return len(offer.PublicUsageConditions) > 0 }) {
			tariff := buildCatalogModelTariff(effectivePricingPageContext{groupRatios: groupRatios, fxFactor: fxFactor}, item, group)
			for index := range operations {
				for _, operation := range tariff.Operations {
					if operations[index].Operation == operation.Operation && operations[index].Mode == operation.Mode {
						operations[index].Tiers = operation.Tiers
						operations[index].IsFrom = &operation.IsFrom
					}
				}
			}
		}
		operations = slices.DeleteFunc(operations, func(operation consumerPricingOperation) bool {
			return item.CatalogModality != "" && operation.Mode == "batch" && !confirmedConsumerPrices(operation.Prices, operation.Tiers)
		})
		result.GroupPrices = append(result.GroupPrices, consumerGroupPricing{UsingGroup: group, Operations: operations, ProviderOffers: providers})
	}
	return result
}

func confirmedConsumerPrices(prices []consumerUnitPrice, tiers []publicPricingTier) bool {
	for _, price := range prices {
		if price.AmountRub != nil && *price.AmountRub >= 0 && !math.IsNaN(*price.AmountRub) && !math.IsInf(*price.AmountRub, 0) && price.Unit != "" && price.Unit != "unknown" {
			return true
		}
	}
	for _, tier := range tiers {
		if confirmedConsumerPrices(tier.Prices, nil) {
			return true
		}
	}
	return false
}

func consumerOperations(item model.Pricing, group string, ratio float64, validRatio bool) []consumerPricingOperation {
	candidates := make(map[string][]model.CatalogOffer)
	catalogOffers := item.CatalogOffersForGroup(group)
	for _, offer := range catalogOffers {
		key := offer.Operation + ":" + offer.Mode
		candidates[key] = append(candidates[key], offer)
	}
	keys := make([]string, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]consumerPricingOperation, 0, len(keys))
	for _, key := range keys {
		operation, mode, _ := strings.Cut(key, ":")
		selected := model.SelectCatalogOffer(candidates[key])
		row := consumerPricingOperation{Operation: operation, Mode: mode, Prices: []consumerUnitPrice{}}
		if selected != nil && validRatio {
			for _, price := range selected.Prices {
				if price.AmountUSD == nil || price.Unit == "" || price.Unit == "unknown" || *price.AmountUSD < 0 {
					continue
				}
				amount := *price.AmountUSD * 100 * ratio
				if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
					continue
				}
				row.Prices = append(row.Prices, consumerUnitPrice{Component: price.Component, Unit: price.Unit, AmountRub: &amount})
			}
		}
		components := []string{"base", "input", "output", "cache_read", "cache_write", "cache_write_1h", "image_input", "image_output", "audio_input", "audio_cache_read", "audio_output", "video_output", "reference_input", "additional_reference_input", "font_input"}
		slices.SortStableFunc(row.Prices, func(left, right consumerUnitPrice) int {
			leftIndex, rightIndex := slices.Index(components, left.Component), slices.Index(components, right.Component)
			if leftIndex < 0 {
				leftIndex = len(components)
			}
			if rightIndex < 0 {
				rightIndex = len(components)
			}
			if leftIndex != rightIndex {
				return leftIndex - rightIndex
			}
			return strings.Compare(left.Component, right.Component)
		})
		result = append(result, row)
	}
	return result
}

func consumerOperation(endpoint constant.EndpointType) (string, string) {
	switch endpoint {
	case model.PricingEndpointBatch, constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact:
		return "text", "output"
	case constant.EndpointTypeJinaRerank:
		return "rerank", "input"
	case constant.EndpointTypeEmbeddings:
		return "embeddings", "input"
	case constant.EndpointTypeImageGeneration:
		return "image_generation", "image_output"
	case constant.EndpointTypeOpenAIVideo:
		return "text_to_video", "video_output"
	}
	return "", ""
}
