package controller

import (
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

type consumerPricingResponse struct {
	SchemaVersion string                 `json:"schema_version"`
	UserGroup     string                 `json:"user_group"`
	Currency      string                 `json:"currency"`
	AutoGroups    []string               `json:"auto_groups"`
	Data          []consumerPricingModel `json:"data"`
}

type consumerPricingModel struct {
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
	UsingGroup string                     `json:"using_group"`
	Operations []consumerPricingOperation `json:"operations"`
}

type consumerPricingOperation struct {
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
	for _, item := range filterPricingByUsableGroups(pricing, usableGroups) {
		response.Data = append(response.Data, buildConsumerPricingModel(usableGroups, groupRatios, basis.Factor, item))
	}
	return response
}

func buildConsumerPricingModel(usableGroups map[string]string, groupRatios map[string]float64, fxFactor float64, item model.Pricing) consumerPricingModel {
	result := consumerPricingModel{ModelName: item.ModelName, Description: item.Description, Icon: item.Icon, Tags: item.Tags, VendorID: item.VendorID, OwnerBy: item.OwnerBy, SupportedEndpointTypes: item.SupportedEndpointTypes, GroupPrices: []consumerGroupPricing{}}
	groups := effectivePricingGroups(item.EnableGroup, usableGroups)
	slices.Sort(groups)
	for _, group := range groups {
		ratio, err := service.ApplyBillingFX(groupRatios[group], fxFactor)
		operations := consumerOperations(item, group, ratio, err == nil)
		result.GroupPrices = append(result.GroupPrices, consumerGroupPricing{UsingGroup: group, Operations: operations})
	}
	return result
}

func consumerOperations(item model.Pricing, group string, ratio float64, validRatio bool) []consumerPricingOperation {
	candidates := make(map[string][]model.CatalogOffer)
	for _, offer := range item.CatalogOffersForGroup(group) {
		key := offer.Operation + ":" + offer.Mode
		candidates[key] = append(candidates[key], offer)
	}
	for endpoint, sources := range item.BillingSourcesForGroup(group) {
		operation, main := consumerOperation(endpoint)
		if endpoint == constant.EndpointTypeOpenAI {
			name := strings.ToLower(item.ModelName[strings.LastIndex(item.ModelName, "/")+1:])
			switch {
			case pricingSpeechFamily.MatchString(name):
				operation, main = "speech_generation", "audio_output"
			case pricingTranscriptionFamily.MatchString(name):
				operation, main = "transcription", "audio_input"
			case strings.HasPrefix(name, "gpt-image-"), strings.HasPrefix(name, "chatgpt-image-"), strings.HasPrefix(name, "dall-e-"):
				operation, main = "image_generation", "image_output"
			}
		}
		if operation == "" {
			continue
		}
		mode := "sync"
		if endpoint == model.PricingEndpointBatch {
			mode = "batch"
		}
		key := operation + ":" + mode
		if mode == "batch" && len(candidates[key]) == 0 {
			expression, _ := billing_setting.GetBatchBillingExpr(item.ModelName)
			if expression == "" {
				continue
			}
		}
		if !slices.Contains(sources, model.BillingSourceConfiguredTariff) && !slices.Contains(item.SupportedEndpointTypes, endpoint) {
			continue
		}
		if _, exists := candidates[key]; !exists {
			candidates[key] = nil
		}
		if !slices.Contains(sources, model.BillingSourceConfiguredTariff) {
			continue
		}
		expression := item.BillingExpr
		useExpression := item.BillingMode == billing_setting.BillingModeTieredExpr
		if mode == "batch" {
			expression, _ = billing_setting.GetBatchBillingExpr(item.ModelName)
			useExpression = true
		}
		if useExpression {
			for _, prices := range item.ConfiguredPrices[expression] {
				offer := model.CatalogOffer{Operation: operation, Mode: mode, MainComponent: main, ID: "configured:" + expression}
				if len(prices) == 1 && prices[0].Component == "base" {
					offer.MainComponent = "base"
				}
				for _, price := range prices {
					offer.Prices = append(offer.Prices, model.CatalogPrice{Component: componentName(price.Component), Unit: price.Unit, AmountUSD: &price.AmountUSD})
				}
				candidates[key] = append(candidates[key], offer)
			}
			continue
		}
		if endpoint == constant.EndpointTypeOpenAIVideo && item.QuotaType == 1 && item.PriceConfigured {
			basis, known := item.LegacyVideoPriceBasisForGroup(group)
			if !known {
				continue
			}
			delete(candidates, key)
			for action, unitBasis := range basis {
				amount := item.ModelPrice
				if slices.Contains(constant.TaskPricePatches, item.ModelName) {
					unitBasis.Unit = "call"
				}
				offer := model.CatalogOffer{ID: "configured:" + action, Operation: action, Mode: mode, MainComponent: "video_output", Prices: []model.CatalogPrice{{Component: "video_output", Unit: unitBasis.Unit, AmountUSD: &amount}}}
				candidates[action+":"+mode] = append(candidates[action+":"+mode], offer)
			}
			continue
		}
		if !item.PriceConfigured {
			continue
		}
		if operation == "speech_generation" || operation == "transcription" || operation == "image_generation" {
			continue
		}
		if item.QuotaType == 1 {
			amount := item.ModelPrice
			candidates[key] = append(candidates[key], model.CatalogOffer{ID: "configured", Operation: operation, Mode: mode, MainComponent: "base", Prices: []model.CatalogPrice{{Component: "base", Unit: "call", AmountUSD: &amount}}})
		} else if operation == "text" || operation == "embeddings" || operation == "rerank" {
			input, output := item.ModelRatio*2, item.ModelRatio*item.CompletionRatio*2
			prices := []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: &input}, {Component: "output", Unit: "million_tokens", AmountUSD: &output}}
			for _, extra := range []struct {
				component string
				ratio     *float64
			}{{"cache_read", item.CacheRatio}, {"cache_write", item.CreateCacheRatio}, {"image_input", item.ImageRatio}, {"audio_input", item.AudioRatio}} {
				if extra.ratio != nil {
					amount := input * *extra.ratio
					prices = append(prices, model.CatalogPrice{Component: extra.component, Unit: "million_tokens", AmountUSD: &amount})
				}
			}
			candidates[key] = append(candidates[key], model.CatalogOffer{ID: "configured", Operation: operation, Mode: mode, MainComponent: main, Prices: prices})
		}
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
				var amount *float64
				if price.AmountUSD != nil {
					converted := *price.AmountUSD * 100 * ratio
					if converted >= 0 && !math.IsNaN(converted) && !math.IsInf(converted, 0) {
						amount = &converted
					}
				}
				row.Prices = append(row.Prices, consumerUnitPrice{Component: price.Component, Unit: price.Unit, AmountRub: amount})
			}
		}
		components := []string{"base", "input", "output", "cache_read", "cache_write", "cache_write_1h", "image_input", "image_output", "audio_input", "audio_output", "video_output", "reference_input", "additional_reference_input", "font_input"}
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
