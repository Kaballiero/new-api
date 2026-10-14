package openrouter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/helper"
)

const maxEndpointPricingOverrides = 128
const maxEndpointPricingCalendarContexts = 4096

type endpointPricingOverride struct {
	threshold    int
	thresholdSet bool
	start, end   int
	clock        bool
	days         []string
	prices       map[string]json.RawMessage
}

func endpointPricingClock(raw json.RawMessage) (int, error) {
	var clock int
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || common.Unmarshal(raw, &clock) != nil || clock < 0 || clock > 2359 || clock%100 >= 60 {
		return 0, errors.New("invalid OpenRouter pricing UTC time")
	}
	return clock/100*60 + clock%100, nil
}

func endpointPricingTariffs(endpoint PricedEndpoint, prompt int) ([]PricedEndpoint, error) {
	prices := maps.Clone(endpoint.Pricing)
	raw, hasOverrides := prices["overrides"]
	delete(prices, "overrides")
	endpoint.Pricing = prices
	if !hasOverrides || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return []PricedEndpoint{endpoint}, nil
	}
	var declarations []map[string]json.RawMessage
	if common.Unmarshal(raw, &declarations) != nil || len(declarations) > maxEndpointPricingOverrides {
		return nil, errors.New("invalid OpenRouter endpoint pricing overrides")
	}
	weekdays := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	minutes := []int{0}
	days := weekdays[:1]
	hasCalendar := false
	overrides := make([]endpointPricingOverride, 0, len(declarations))
	for _, declaration := range declarations {
		override := endpointPricingOverride{prices: make(map[string]json.RawMessage)}
		threshold, thresholdExists := declaration["min_prompt_tokens"]
		override.thresholdSet = thresholdExists
		if thresholdExists && (bytes.Equal(bytes.TrimSpace(threshold), []byte("null")) || common.Unmarshal(threshold, &override.threshold) != nil || override.threshold < 0 || override.threshold > helper.MaxTokensLimit) {
			return nil, errors.New("invalid OpenRouter pricing override token threshold")
		}
		start, startExists := declaration["utc_start"]
		end, endExists := declaration["utc_end"]
		if startExists != endExists {
			return nil, errors.New("invalid OpenRouter pricing UTC interval")
		}
		if startExists {
			var err error
			override.start, err = endpointPricingClock(start)
			if err != nil {
				return nil, err
			}
			override.end, err = endpointPricingClock(end)
			if err != nil {
				return nil, err
			}
			if override.start == override.end {
				return nil, errors.New("ambiguous OpenRouter pricing UTC interval")
			}
			override.clock = true
			hasCalendar = true
			minutes = append(minutes, override.start, override.end)
		}
		rawDays, daysExist := declaration["utc_days"]
		if daysExist {
			if common.Unmarshal(rawDays, &override.days) != nil || len(override.days) == 0 || len(override.days) > len(weekdays) {
				return nil, errors.New("invalid OpenRouter pricing UTC days")
			}
			for index, day := range override.days {
				if !slices.Contains(weekdays, day) || slices.Contains(override.days[:index], day) {
					return nil, errors.New("invalid OpenRouter pricing UTC day")
				}
			}
			hasCalendar = true
			days = weekdays
		}
		if !thresholdExists && !startExists && !daysExist {
			return nil, errors.New("OpenRouter pricing override condition is missing")
		}
		for key, value := range declaration {
			switch key {
			case "min_prompt_tokens", "utc_start", "utc_end", "utc_days":
				continue
			case "prompt", "completion", "input_cache_read", "input_cache_write", "input_cache_write_1h", "request", "image", "audio", "input_audio_cache", "internal_reasoning":
			default:
				return nil, fmt.Errorf("OpenRouter pricing override has unsupported dimension %s", key)
			}
			if _, err := readRate(value); err != nil {
				return nil, err
			}
			override.prices[key] = value
		}
		overrides = append(overrides, override)
	}
	if !hasCalendar {
		tariffs := []PricedEndpoint{endpoint}
		for _, override := range overrides {
			if prompt < override.threshold {
				continue
			}
			variant := endpoint
			variant.Pricing = maps.Clone(prices)
			maps.Copy(variant.Pricing, override.prices)
			tariffs = append(tariffs, variant)
		}
		return tariffs, nil
	}
	slices.Sort(minutes)
	minutes = slices.Compact(minutes)
	if len(minutes)*len(days) > maxEndpointPricingCalendarContexts {
		return nil, errors.New("OpenRouter pricing calendar context limit exceeded")
	}
	tariffs := make([]PricedEndpoint, 0, len(minutes)*len(days))
	for _, minute := range minutes {
		for _, day := range days {
			variant := endpoint
			variant.Pricing = maps.Clone(prices)
			for _, override := range overrides {
				if override.thresholdSet && prompt <= override.threshold || len(override.days) > 0 && !slices.Contains(override.days, day) {
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
				maps.Copy(variant.Pricing, override.prices)
			}
			tariffs = append(tariffs, variant)
		}
	}
	return tariffs, nil
}
