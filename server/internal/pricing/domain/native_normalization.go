package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

type NativePricedEndpoint struct {
	Status                                              *int
	ModelID, Tag                                        string
	ContextLength, MaxPromptTokens, MaxCompletionTokens int
	Quantization                                        string
	SupportedParameters                                 []string
	Pricing                                             map[string]json.RawMessage
}
type NativeImageCapability struct {
	Type     string
	Values   []string
	Min, Max int64
}
type NativeImagePrice struct {
	Billable, Unit, Variant string
	CostUSD                 float64
}
type NativeImageEndpoint struct {
	PromptOnly          bool
	ProviderTag         string
	SupportedParameters map[string]NativeImageCapability
	Pricing             []NativeImagePrice
}
type NativeEndpointResponse struct {
	ID                                string
	Endpoints                         []NativePricedEndpoint
	InputModalities, OutputModalities []string
}
type NativeImageResponse struct {
	ID        string
	Endpoints []NativeImageEndpoint
}
type NativeNormalizationRuntime struct {
	Capabilities       Capabilities
	DecodeEndpoint     func([]byte) (NativeEndpointResponse, error)
	DecodeImage        func([]byte) (NativeImageResponse, error)
	ReadPrice          func(json.RawMessage) (float64, error)
	PublishedMuseImage func(string) (NativeImageEndpoint, bool)
	PublishedImage     func(string, NativeImageEndpoint) NativeImageEndpoint
}
type NativeNormalizer struct{ runtime NativeNormalizationRuntime }

func NewNativeNormalizer(runtime NativeNormalizationRuntime) *NativeNormalizer {
	return &NativeNormalizer{runtime: runtime}
}

const nativePricingMaxContexts = 4096

type pricingCatalogOverride struct {
	threshold        int
	thresholdPresent bool
	start, end       int
	clock            bool
	days             []string
	prices           map[string]json.RawMessage
}

func NativePricingClock(deps Capabilities, raw json.RawMessage) (int, error) {
	var clock int
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || deps.Codec.Unmarshal(raw, &clock) != nil || clock < 0 || clock > 2359 || clock%100 >= 60 {
		return 0, errors.New("invalid native pricing UTC time")
	}
	return clock/100*60 + clock%100, nil
}
func NativePricingVectors(deps Capabilities, endpoint NativePricedEndpoint, conditions ...*[][]ExpressionCondition) ([]map[string]json.RawMessage, error) {
	prices := maps.Clone(endpoint.Pricing)
	raw, hasOverrides := prices["overrides"]
	delete(prices, "overrides")
	if !hasOverrides || string(raw) == "null" {
		if len(conditions) > 0 {
			*conditions[0] = [][]ExpressionCondition{nil}
		}
		return []map[string]json.RawMessage{prices}, nil
	}
	var declarations []map[string]json.RawMessage
	if deps.Codec.Unmarshal(raw, &declarations) != nil || len(declarations) > 128 {
		return nil, errors.New("invalid native pricing overrides")
	}
	weekdays := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	thresholds, minutes := []int{0}, []int{0}
	days := weekdays[:1]
	overrides := make([]pricingCatalogOverride, 0, len(declarations))
	for _, declaration := range declarations {
		override := pricingCatalogOverride{prices: make(map[string]json.RawMessage)}
		if threshold, exists := declaration["min_prompt_tokens"]; exists {
			override.thresholdPresent = true
			if bytes.Equal(bytes.TrimSpace(threshold), []byte("null")) || deps.Codec.Unmarshal(threshold, &override.threshold) != nil || override.threshold < 0 {
				return nil, errors.New("invalid native pricing threshold")
			}
		}
		start, startExists := declaration["utc_start"]
		end, endExists := declaration["utc_end"]
		if startExists != endExists {
			return nil, errors.New("invalid native pricing UTC interval")
		}
		if startExists {
			var err error
			override.start, err = NativePricingClock(deps, start)
			if err != nil {
				return nil, err
			}
			override.end, err = NativePricingClock(deps, end)
			if err != nil {
				return nil, err
			}
			if override.start == override.end {
				return nil, errors.New("ambiguous native pricing UTC interval")
			}
			override.clock = true
			minutes = append(minutes, override.start, override.end)
		}
		if rawDays, exists := declaration["utc_days"]; exists {
			if deps.Codec.Unmarshal(rawDays, &override.days) != nil || len(override.days) == 0 || len(override.days) > 7 {
				return nil, errors.New("invalid native pricing UTC days")
			}
			seenDays := make(map[string]bool, len(override.days))
			for _, day := range override.days {
				if !slices.Contains(weekdays, day) || seenDays[day] {
					return nil, errors.New("invalid native pricing UTC day")
				}
				seenDays[day] = true
			}
			days = weekdays
		}
		for key, value := range declaration {
			switch key {
			case "min_prompt_tokens", "utc_start", "utc_end", "utc_days":
			case "prompt", "completion", "input_cache_read", "input_cache_write", "input_cache_write_1h", "request", "image", "audio", "audio_output", "input_audio_cache", "image_output", "internal_reasoning", "web_search":
				override.prices[key] = value
			default:
				return nil, errors.New("unsupported native pricing override condition or dimension")
			}
		}
		if override.thresholdPresent {
			if override.threshold == int(^uint(0)>>1) || endpoint.ContextLength > 0 && override.threshold >= endpoint.ContextLength || endpoint.MaxPromptTokens > 0 && override.threshold >= endpoint.MaxPromptTokens {
				continue
			}
			thresholds = append(thresholds, override.threshold+1)
		}
		overrides = append(overrides, override)
	}
	slices.Sort(thresholds)
	thresholds = slices.Compact(thresholds)
	slices.Sort(minutes)
	minutes = slices.Compact(minutes)
	if len(thresholds)*len(minutes)*len(days) > nativePricingMaxContexts {
		return nil, errors.New("native pricing context limit exceeded")
	}
	vectors := []map[string]json.RawMessage{}
	seen := map[string]int{}
	guards := [][]ExpressionCondition{}
	for thresholdIndex, threshold := range thresholds {
		for minuteIndex, minute := range minutes {
			for _, day := range days {
				vector := maps.Clone(prices)
				for _, override := range overrides {
					if override.thresholdPresent && override.threshold >= threshold || len(override.days) > 0 && !slices.Contains(override.days, day) {
						continue
					}
					if override.clock {
						applies := minute >= override.start && minute < override.end
						if override.start > override.end {
							applies = minute >= override.start || minute < override.end
						}
						if !applies {
							continue
						}
					}
					for key, value := range override.prices {
						vector[key] = value
					}
				}
				encoded, err := deps.Codec.Marshal(vector)
				if err != nil {
					return nil, err
				}
				context := []ExpressionCondition{}
				if len(thresholds) > 1 {
					context = append(context, ExpressionCondition{Var: "p", Op: ">=", Value: float64(threshold)})
					if thresholdIndex+1 < len(thresholds) {
						context = append(context, ExpressionCondition{Var: "p", Op: "<", Value: float64(thresholds[thresholdIndex+1])})
					}
				}
				if len(minutes) > 1 {
					context = append(context, ExpressionCondition{Var: "utc_minute", Arg: "UTC", Op: ">=", Value: float64(minute)})
					upper := 24 * 60
					if minuteIndex+1 < len(minutes) {
						upper = minutes[minuteIndex+1]
					}
					context = append(context, ExpressionCondition{Var: "utc_minute", Arg: "UTC", Op: "<", Value: float64(upper)})
				}
				if len(days) > 1 {
					context = append(context, ExpressionCondition{Var: "weekday", Arg: "UTC", Op: "==", Value: float64((slices.Index(weekdays, day) + 1) % 7)})
				}
				if index, exists := seen[string(encoded)]; exists {
					alternative := ExpressionCondition{Op: "&&", Operands: context}
					if len(guards[index]) == 1 && guards[index][0].Op == "||" {
						guards[index][0].Operands = append(guards[index][0].Operands, alternative)
					} else {
						guards[index] = []ExpressionCondition{{Op: "||", Operands: []ExpressionCondition{{Op: "&&", Operands: guards[index]}, alternative}}}
					}
				} else {
					seen[string(encoded)] = len(vectors)
					vectors = append(vectors, vector)
					guards = append(guards, context)
				}
			}
		}
	}
	if len(conditions) > 0 {
		*conditions[0] = guards
	}
	return vectors, nil
}
func SelectPricingCatalogOffers(offers []CatalogOffer) []CatalogOffer {
	groups := make(map[string][]CatalogOffer)
	for _, offer := range offers {
		unit := "unknown"
		for _, price := range offer.Prices {
			if price.Component == offer.MainComponent {
				unit = price.Unit
				break
			}
		}
		key := offer.Operation + ":" + offer.Mode + ":" + unit
		groups[key] = append(groups[key], offer)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := []CatalogOffer{}
	for _, key := range keys {
		selected := SelectCatalogOffer(groups[key])
		if selected == nil {
			unknown := groups[key][0]
			unknown.Prices = nil
			selected = &unknown
		}
		chosen := *selected
		chosen.PublicAlternatives = slices.Clone(groups[key])
		result = append(result, chosen)
	}
	return result
}
func (normalizer *NativeNormalizer) NormalizeEndpointPricing(upstream, operation, main string, body []byte) ([]CatalogOffer, error) {
	decoded, decodeErr := normalizer.runtime.DecodeEndpoint(body)
	response := struct{ Data NativeEndpointResponse }{Data: decoded}
	if decodeErr != nil || response.Data.ID != upstream || response.Data.Endpoints == nil {
		return nil, errors.New("invalid native endpoint pricing response")
	}

	for _, endpoint := range response.Data.Endpoints {
		if strings.Contains(upstream, ":") && endpoint.ModelID != upstream {
			return nil, errors.New("native variant endpoint identity mismatch")
		}
	}
	if operation == "text" && (!slices.Contains(response.Data.InputModalities, "text") || !slices.Contains(response.Data.OutputModalities, "text")) {
		if len(response.Data.InputModalities) == 0 || len(response.Data.OutputModalities) == 0 {
			return nil, errors.New("native endpoint has no proved operation")
		}
		return []CatalogOffer{}, nil
	}
	offers := []CatalogOffer{}
	if len(response.Data.Endpoints) == 0 {
		offers = append(offers, CatalogOffer{ID: "native:unknown", Operation: operation, Mode: "sync", MainComponent: main})
	}
	for index, endpoint := range response.Data.Endpoints {
		var conditions [][]ExpressionCondition
		vectors, err := NativePricingVectors(normalizer.runtime.Capabilities, endpoint, &conditions)
		if err != nil {
			return nil, err
		}
		for variant, vector := range vectors {
			offer := CatalogOffer{PublicConditions: conditions[variant], PublicConditional: len(conditions[variant]) > 0, ID: fmt.Sprintf("%s:%d:%d", endpoint.Tag, index, variant), Operation: operation, Mode: "sync", MainComponent: main, Prices: []CatalogPrice{}}
			keys := make([]string, 0, len(vector))
			for key := range vector {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			unprojectable := false
			for _, key := range keys {
				component, unit, multiplier := "", "million_tokens", 1e6
				switch key {
				case "prompt":
					component = "input"
				case "completion":
					component = "output"
				case "input_cache_read":
					component = "cache_read"
				case "input_cache_write":
					component = "cache_write"
				case "input_cache_write_1h":
					component = "cache_write_1h"
				case "internal_reasoning":
					component = "reasoning_output"
				case "web_search":
					component, unit, multiplier = "web_search", "search", 1
					amount, err := normalizer.runtime.ReadPrice(vector[key])
					if err != nil {
						return nil, err
					}
					if amount == 0 && !slices.Contains(endpoint.SupportedParameters, "web_search_options") {
						continue
					}
				case "request":
					component = "base"
					unit = "call"
					multiplier = 1
				case "image":
					component = "image_input"
					unit = "image"
					multiplier = 1
				case "discount":
					continue
				case "audio", "input_audio_cache":
					if !slices.Contains(response.Data.InputModalities, "audio") {
						if _, err := normalizer.runtime.ReadPrice(vector[key]); err != nil {
							return nil, err
						}
						continue
					}
					component = "audio_input"
					if key == "input_audio_cache" {
						component = "audio_cache_read"
					}
				case "audio_output":
					if !slices.Contains(response.Data.OutputModalities, "audio") {
						if _, err := normalizer.runtime.ReadPrice(vector[key]); err != nil {
							return nil, err
						}
						continue
					}
					component = "audio_output"
				case "image_output":
					if _, err := normalizer.runtime.ReadPrice(vector[key]); err != nil {
						return nil, err
					}

					continue
				default:
					amount, err := normalizer.runtime.ReadPrice(vector[key])
					if err != nil {
						return nil, err
					}
					if amount != 0 {
						unprojectable = true
					}
					continue
				}
				amount, err := normalizer.runtime.ReadPrice(vector[key])
				if err != nil {
					return nil, err
				}
				amount *= multiplier
				offer.Prices = append(offer.Prices, CatalogPrice{Component: component, Unit: unit, AmountUSD: &amount})
			}
			if unprojectable {
				offer.Prices = nil
			}
			offers = append(offers, offer)
		}
	}
	return offers, nil
}
func (normalizer *NativeNormalizer) NormalizeImagePricing(upstream string, body []byte) ([]CatalogOffer, error) {
	response, decodeErr := normalizer.runtime.DecodeImage(body)
	if decodeErr != nil || response.ID != upstream || response.Endpoints == nil {
		return nil, errors.New("invalid native image pricing response")
	}

	// The native float DTO alone cannot distinguish a missing cost from free.
	var rawResponse struct {
		Endpoints []struct {
			Pricing []map[string]json.RawMessage `json:"pricing"`
		} `json:"endpoints"`
	}
	if normalizer.runtime.Capabilities.Codec.Unmarshal(body, &rawResponse) != nil {
		return nil, errors.New("invalid native image rates")
	}
	offers := []CatalogOffer{}
	if len(response.Endpoints) == 0 {
		if endpoint, known := normalizer.runtime.PublishedMuseImage(upstream); known {
			response.Endpoints = []NativeImageEndpoint{endpoint}
		}
	}
	for index, endpoint := range response.Endpoints {
		published := len(endpoint.Pricing) == 0
		endpoint = normalizer.runtime.PublishedImage(upstream, endpoint)
		variants := map[string]bool{"": true}
		for priceIndex, price := range endpoint.Pricing {
			if index < len(rawResponse.Endpoints) && !published {
				if _, err := normalizer.runtime.ReadPrice(rawResponse.Endpoints[index].Pricing[priceIndex]["cost_usd"]); err != nil {
					return nil, err
				}
			}
			if price.Variant != "" && ImagePricingVariantSupported(price.Variant, endpoint.SupportedParameters) {
				variants[price.Variant] = true
			}
		}
		for variant := range variants {
			offer := CatalogOffer{ID: fmt.Sprintf("%s:%d:%s", endpoint.ProviderTag, index, variant), Operation: "image_generation", Mode: "sync", MainComponent: "image_output", Prices: []CatalogPrice{}}
			components := make(map[string]string)
			for _, price := range endpoint.Pricing {
				if price.Variant != "" && price.Variant != variant {
					continue
				}
				component := ""
				switch price.Billable {
				case "output_image":
					component = "image_output"
				case "input_image":
					component = "image_input"
				case "input_reference":
					component = "reference_input"
				case "input_text":
					component = "input"
				case "input_font":
					component = "font_input"
				default:
					return nil, errors.New("unsupported native image billable")
				}
				unit, amount := price.Unit, price.CostUSD
				switch unit {
				case "token":
					unit = "million_tokens"
					amount *= 1e6
				case "request":
					unit = "call"
				case "image", "megapixel":
				default:
					return nil, errors.New("unsupported native image unit")
				}
				if existingVariant, exists := components[component]; exists {
					if existingVariant == price.Variant {
						return nil, errors.New("ambiguous native image price vector")
					}
					if price.Variant == "" {
						continue
					}
					for index := range offer.Prices {
						if offer.Prices[index].Component == component {
							offer.Prices[index] = CatalogPrice{Component: component, Unit: unit, AmountUSD: &amount}
						}
					}
					components[component] = price.Variant
					continue
				}
				components[component] = price.Variant
				offer.Prices = append(offer.Prices, CatalogPrice{Component: component, Unit: unit, AmountUSD: &amount})
			}
			offers = append(offers, offer)
		}
	}
	if len(offers) == 0 {
		offers = append(offers, CatalogOffer{ID: "native:image:unknown", Operation: "image_generation", Mode: "sync", MainComponent: "image_output"})
	}
	return offers, nil
}
func ImagePricingVariantSupported(variant string, parameters map[string]NativeImageCapability) bool {
	for _, part := range strings.Split(variant, "_") {
		found := false
		for _, capability := range parameters {
			if capability.Type != "enum" {
				continue
			}
			for _, value := range capability.Values {
				if strings.EqualFold(part, value) {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (n *NativeNormalizer) DecodeImageMetadata(body []byte) (NativeImageResponse, error) {
	return n.runtime.DecodeImage(body)
}
