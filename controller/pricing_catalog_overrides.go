package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
)

const pricingCatalogMaxContexts = 4096

type pricingCatalogOverride struct {
	threshold        int
	thresholdPresent bool
	start, end       int
	clock            bool
	days             []string
	prices           map[string]json.RawMessage
}

func nativePricingClock(raw json.RawMessage) (int, error) {
	var clock int
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || common.Unmarshal(raw, &clock) != nil || clock < 0 || clock > 2359 || clock%100 >= 60 {
		return 0, errors.New("invalid native pricing UTC time")
	}
	return clock/100*60 + clock%100, nil
}

func nativePricingVectors(endpoint openrouter.PricedEndpoint, conditions ...*[][]billingexpr.TierCondition) ([]map[string]json.RawMessage, error) {
	prices := maps.Clone(endpoint.Pricing)
	raw, hasOverrides := prices["overrides"]
	delete(prices, "overrides")
	if !hasOverrides || string(raw) == "null" {
		if len(conditions) > 0 {
			*conditions[0] = [][]billingexpr.TierCondition{nil}
		}
		return []map[string]json.RawMessage{prices}, nil
	}
	var declarations []map[string]json.RawMessage
	if common.Unmarshal(raw, &declarations) != nil || len(declarations) > 128 {
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
			if bytes.Equal(bytes.TrimSpace(threshold), []byte("null")) || common.Unmarshal(threshold, &override.threshold) != nil || override.threshold < 0 {
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
			override.start, err = nativePricingClock(start)
			if err != nil {
				return nil, err
			}
			override.end, err = nativePricingClock(end)
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
			if common.Unmarshal(rawDays, &override.days) != nil || len(override.days) == 0 || len(override.days) > 7 {
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
	if len(thresholds)*len(minutes)*len(days) > pricingCatalogMaxContexts {
		return nil, errors.New("native pricing context limit exceeded")
	}
	vectors := []map[string]json.RawMessage{}
	seen := map[string]int{}
	guards := [][]billingexpr.TierCondition{}
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
				encoded, err := common.Marshal(vector)
				if err != nil {
					return nil, err
				}
				context := []billingexpr.TierCondition{}
				if len(thresholds) > 1 {
					context = append(context, billingexpr.TierCondition{Var: "p", Op: ">=", Value: float64(threshold)})
					if thresholdIndex+1 < len(thresholds) {
						context = append(context, billingexpr.TierCondition{Var: "p", Op: "<", Value: float64(thresholds[thresholdIndex+1])})
					}
				}
				if len(minutes) > 1 {
					context = append(context, billingexpr.TierCondition{Var: "utc_minute", Arg: "UTC", Op: ">=", Value: float64(minute)})
					upper := 24 * 60
					if minuteIndex+1 < len(minutes) {
						upper = minutes[minuteIndex+1]
					}
					context = append(context, billingexpr.TierCondition{Var: "utc_minute", Arg: "UTC", Op: "<", Value: float64(upper)})
				}
				if len(days) > 1 {
					context = append(context, billingexpr.TierCondition{Var: "weekday", Arg: "UTC", Op: "==", Value: float64((slices.Index(weekdays, day) + 1) % 7)})
				}
				if index, exists := seen[string(encoded)]; exists {
					alternative := billingexpr.TierCondition{Op: "&&", Operands: context}
					if len(guards[index]) == 1 && guards[index][0].Op == "||" {
						guards[index][0].Operands = append(guards[index][0].Operands, alternative)
					} else {
						guards[index] = []billingexpr.TierCondition{{Op: "||", Operands: []billingexpr.TierCondition{{Op: "&&", Operands: guards[index]}, alternative}}}
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
