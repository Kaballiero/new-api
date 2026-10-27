package domain

import (
	"bytes"

	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/samber/lo"
)

var pricingSyncFields = []string{
	"model_ratio",
	"completion_ratio",
	"cache_ratio",
	"create_cache_ratio",
	"image_ratio",
	"audio_ratio",
	"audio_completion_ratio",
	"model_price",
	"billing_mode",
	"billing_expr",
}
var numericPricingSyncFields = map[string]bool{
	"model_ratio":            true,
	"completion_ratio":       true,
	"cache_ratio":            true,
	"create_cache_ratio":     true,
	"image_ratio":            true,
	"audio_ratio":            true,
	"audio_completion_ratio": true,
	"model_price":            true,
}

const (
	defaultTimeoutSeconds       = 10
	defaultEndpoint             = "/api/pricing"
	maxConcurrentFetches        = 8
	maxRatioConfigBytes         = 10 << 20 // 10MB
	floatEpsilon                = 1e-9
	officialRatioPresetID       = -100
	officialRatioPresetName     = "官方倍率预设"
	officialRatioPresetBaseURL  = "https://basellm.github.io"
	modelsDevPresetID           = -101
	modelsDevPresetName         = "models.dev 价格预设"
	modelsDevPresetBaseURL      = "https://models.dev"
	modelsDevHost               = "models.dev"
	modelsDevPath               = "/api.json"
	modelsDevInputCostRatioBase = 1000.0
)

type UpstreamResult struct {
	Name string         `json:"name"`
	Data map[string]any `json:"data,omitempty"`
	Err  error          `json:"-"`
}
type ModelsDevProvider struct {
	Models map[string]ModelsDevModel `json:"models"`
}
type ModelsDevModel struct {
	Cost ModelsDevCost `json:"cost"`
}
type ModelsDevCost struct {
	Input     *float64 `json:"input"`
	Output    *float64 `json:"output"`
	CacheRead *float64 `json:"cache_read"`
}
type ModelsDevCandidate struct {
	Provider  string
	Input     float64
	Output    *float64
	CacheRead *float64
}

func NearlyEqual(a, b float64) bool {
	if a > b {
		return a-b < floatEpsilon
	}
	return b-a < floatEpsilon
}
func ValuesEqual(a, b any) bool {
	af, aok := a.(float64)
	bf, bok := b.(float64)
	if aok && bok {
		return NearlyEqual(af, bf)
	}
	return a == b
}
func ValueMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]float64:
		return lo.MapValues(typed, func(value float64, _ string) any { return value })
	case map[string]string:
		return lo.MapValues(typed, func(value string, _ string) any { return value })
	default:
		return nil
	}
}
func AsFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
func NormalizeSyncValue(field string, value any) any {
	if numericPricingSyncFields[field] {
		if parsed, ok := AsFloat64(value); ok {
			return parsed
		}
	}
	return value
}

// effectivePricingSyncData follows the billing engine's mode precedence. An
// inactive expression and numeric settings covered by an active expression
// are not separate prices and must not appear as synchronization differences.
func EffectivePricingSyncData(data map[string]any) map[string]any {
	result := make(map[string]any, len(pricingSyncFields))
	names := make(map[string]struct{})
	for _, field := range pricingSyncFields {
		entries := make(map[string]any)
		for name, raw := range ValueMap(data[field]) {
			value := NormalizeSyncValue(field, raw)
			if numericPricingSyncFields[field] {
				number, ok := value.(float64)
				if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
					continue
				}
			}
			entries[name] = value
			names[name] = struct{}{}
		}
		result[field] = entries
	}
	modes := ValueMap(result["billing_mode"])
	expressions := ValueMap(result["billing_expr"])
	for name := range names {
		expression, _ := expressions[name].(string)
		if modes[name] == BillingModeTieredExpr {
			if strings.TrimSpace(expression) == "" {
				for _, field := range pricingSyncFields {
					delete(ValueMap(result[field]), name)
				}
				continue
			}
			expressions[name] = strings.TrimSpace(expression)
			for field := range numericPricingSyncFields {
				delete(ValueMap(result[field]), name)
			}
			continue
		}
		delete(expressions, name)
		modes[name] = BillingModeRatio
		_, fixed := ValueMap(result["model_price"])[name]
		_, token := ValueMap(result["model_ratio"])[name]
		if !fixed && !token {
			for _, field := range pricingSyncFields {
				delete(ValueMap(result[field]), name)
			}
			continue
		}
		if fixed {
			for field := range numericPricingSyncFields {
				if field != "model_price" {
					delete(ValueMap(result[field]), name)
				}
			}
		}
	}
	return result
}
func ModelPricingSyncValues(data map[string]any, name string) map[string]any {
	values := make(map[string]any)
	for _, field := range pricingSyncFields {
		if value, exists := ValueMap(data[field])[name]; exists {
			values[field] = value
		}
	}
	return values
}
func BuildDifferences(localData map[string]any, successfulChannels []SyncChannel) map[string]map[string]RatioDifference {
	differences := make(map[string]map[string]RatioDifference)
	localData = EffectivePricingSyncData(localData)
	normalizedChannels := make([]SyncChannel, 0, len(successfulChannels))
	for _, channel := range successfulChannels {
		channel.Data = EffectivePricingSyncData(channel.Data)
		normalizedChannels = append(normalizedChannels, channel)
	}
	successfulChannels = normalizedChannels

	allModels := make(map[string]struct{})

	for _, field := range pricingSyncFields {
		for modelName := range ValueMap(localData[field]) {
			allModels[modelName] = struct{}{}
		}
	}

	for _, channel := range successfulChannels {
		for _, field := range pricingSyncFields {
			for modelName := range ValueMap(channel.Data[field]) {
				allModels[modelName] = struct{}{}
			}
		}
	}

	confidenceMap := make(map[string]map[string]bool)

	for _, channel := range successfulChannels {
		confidenceMap[channel.Name] = make(map[string]bool)

		modelRatios := ValueMap(channel.Data["model_ratio"])
		completionRatios := ValueMap(channel.Data["completion_ratio"])

		if len(modelRatios) > 0 && len(completionRatios) > 0 {

			for modelName := range allModels {

				confidenceMap[channel.Name][modelName] = true

				if modelRatioVal, ok := modelRatios[modelName]; ok {
					if completionRatioVal, ok := completionRatios[modelName]; ok {

						modelRatioFloat, modelRatioOK := AsFloat64(modelRatioVal)
						completionRatioFloat, completionRatioOK := AsFloat64(completionRatioVal)
						if modelRatioOK && completionRatioOK && NearlyEqual(modelRatioFloat, 37.5) && NearlyEqual(completionRatioFloat, 1.0) {
							confidenceMap[channel.Name][modelName] = false
						}
					}
				}
			}
		} else {

			for modelName := range allModels {
				confidenceMap[channel.Name][modelName] = true
			}
		}
	}

	for modelName := range allModels {
		expressionPriority := ValueMap(localData["billing_mode"])[modelName] == BillingModeTieredExpr
		for _, channel := range successfulChannels {
			if ValueMap(channel.Data["billing_mode"])[modelName] == BillingModeTieredExpr {
				expressionPriority = true
			}
		}
		for _, ratioType := range pricingSyncFields {
			if expressionPriority && numericPricingSyncFields[ratioType] {
				continue
			}
			var localValue any = nil
			if val, exists := ValueMap(localData[ratioType])[modelName]; exists {
				localValue = NormalizeSyncValue(ratioType, val)
			}

			upstreamValues := make(map[string]any)
			confidenceValues := make(map[string]bool)
			hasUpstreamValue := false
			hasDifference := false

			for _, channel := range successfulChannels {
				if expressionPriority && ValueMap(channel.Data["billing_mode"])[modelName] != BillingModeTieredExpr {
					continue
				}
				var upstreamValue any = nil

				if val, exists := ValueMap(channel.Data[ratioType])[modelName]; exists {
					upstreamValue = NormalizeSyncValue(ratioType, val)
					hasUpstreamValue = true

					if localValue != nil && !ValuesEqual(localValue, upstreamValue) {
						hasDifference = true
					} else if ValuesEqual(localValue, upstreamValue) {
						upstreamValue = "same"
					}
				}
				if upstreamValue == nil && localValue == nil {
					upstreamValue = "same"
				}

				if localValue == nil && upstreamValue != nil && upstreamValue != "same" {
					hasDifference = true
				}

				upstreamValues[channel.Name] = upstreamValue

				confidenceValues[channel.Name] = confidenceMap[channel.Name][modelName]
			}

			shouldInclude := false

			if localValue != nil {
				if hasDifference {
					shouldInclude = true
				}
			} else {
				if hasUpstreamValue {
					shouldInclude = true
				}
			}

			if shouldInclude {
				if differences[modelName] == nil {
					differences[modelName] = make(map[string]RatioDifference)
				}
				differences[modelName][ratioType] = RatioDifference{
					Current:    localValue,
					Upstreams:  upstreamValues,
					Confidence: confidenceValues,
				}
			}
		}
	}

	channelHasDiff := make(map[string]bool)
	for _, ratioMap := range differences {
		for _, item := range ratioMap {
			for chName, val := range item.Upstreams {
				if val != nil && val != "same" {
					channelHasDiff[chName] = true
				}
			}
		}
	}

	for modelName, ratioMap := range differences {
		for ratioType, item := range ratioMap {
			for chName := range item.Upstreams {
				if !channelHasDiff[chName] {
					delete(item.Upstreams, chName)
					delete(item.Confidence, chName)
				}
			}

			allSame := true
			for _, v := range item.Upstreams {
				if v != "same" {
					allSame = false
					break
				}
			}
			if len(item.Upstreams) == 0 || allSame {
				delete(ratioMap, ratioType)
			} else {
				differences[modelName][ratioType] = item
			}
		}

		if len(ratioMap) == 0 {
			delete(differences, modelName)
		}
	}

	return differences
}
func RoundRatioValue(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}
func IsModelsDevAPIEndpoint(rawURL string) bool {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if strings.ToLower(parsedURL.Hostname()) != modelsDevHost {
		return false
	}
	path := strings.TrimSuffix(parsedURL.Path, "/")
	if path == "" {
		path = "/"
	}
	return path == modelsDevPath
}

// convertOpenRouterToRatioData parses OpenRouter's /v1/models response and converts
// per-token USD pricing into the local ratio format.
// model_ratio = prompt_price_per_token * 1_000_000 * (USD / 1000)
//
//	since 1 ratio unit = $0.002/1K tokens and USD=500, the factor is 500_000
//
// completion_ratio = completion_price / prompt_price (output/input multiplier)
func ConvertOpenRouterToRatioData(deps Capabilities, reader io.Reader) (map[string]any, error) {
	var orResp struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt         string `json:"prompt"`
				Completion     string `json:"completion"`
				InputCacheRead string `json:"input_cache_read"`
			} `json:"pricing"`
		} `json:"data"`
	}

	if err := deps.Codec.DecodeJson(reader, &orResp); err != nil {
		return nil, fmt.Errorf("failed to decode OpenRouter response: %w", err)
	}

	modelRatioMap := make(map[string]any)
	completionRatioMap := make(map[string]any)
	cacheRatioMap := make(map[string]any)

	for _, m := range orResp.Data {
		promptPrice, promptErr := strconv.ParseFloat(m.Pricing.Prompt, 64)
		completionPrice, compErr := strconv.ParseFloat(m.Pricing.Completion, 64)

		if promptErr != nil && compErr != nil {

			continue
		}

		if promptErr != nil {
			promptPrice = 0
		}
		if compErr != nil {
			completionPrice = 0
		}

		if promptPrice < 0 || completionPrice < 0 {
			continue
		}

		if promptPrice == 0 && completionPrice == 0 {

			modelRatioMap[m.ID] = 0.0
			continue
		}
		if promptPrice <= 0 {

			continue
		}

		ratio := promptPrice * 1000 * 500
		ratio = RoundRatioValue(ratio)
		modelRatioMap[m.ID] = ratio

		compRatio := completionPrice / promptPrice
		compRatio = RoundRatioValue(compRatio)
		completionRatioMap[m.ID] = compRatio

		if m.Pricing.InputCacheRead != "" {
			if cachePrice, err := strconv.ParseFloat(m.Pricing.InputCacheRead, 64); err == nil && cachePrice >= 0 {
				cacheRatio := cachePrice / promptPrice
				cacheRatio = RoundRatioValue(cacheRatio)
				cacheRatioMap[m.ID] = cacheRatio
			}
		}
	}

	converted := make(map[string]any)
	if len(modelRatioMap) > 0 {
		converted["model_ratio"] = modelRatioMap
	}
	if len(completionRatioMap) > 0 {
		converted["completion_ratio"] = completionRatioMap
	}
	if len(cacheRatioMap) > 0 {
		converted["cache_ratio"] = cacheRatioMap
	}

	return converted, nil
}
func CloneFloatPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
func IsValidNonNegativeCost(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	return v >= 0
}
func BuildModelsDevCandidate(provider string, cost ModelsDevCost) (ModelsDevCandidate, bool) {
	if cost.Input == nil {
		return ModelsDevCandidate{}, false
	}

	input := *cost.Input
	if !IsValidNonNegativeCost(input) {
		return ModelsDevCandidate{}, false
	}

	var output *float64
	if cost.Output != nil {
		if !IsValidNonNegativeCost(*cost.Output) {
			return ModelsDevCandidate{}, false
		}
		output = CloneFloatPtr(cost.Output)
	}

	if input == 0 && output != nil && *output > 0 {
		return ModelsDevCandidate{}, false
	}

	var cacheRead *float64
	if cost.CacheRead != nil && IsValidNonNegativeCost(*cost.CacheRead) {
		cacheRead = CloneFloatPtr(cost.CacheRead)
	}

	return ModelsDevCandidate{
		Provider:  provider,
		Input:     input,
		Output:    output,
		CacheRead: cacheRead,
	}, true
}
func ShouldReplaceModelsDevCandidate(current, next ModelsDevCandidate) bool {
	currentNonZero := current.Input > 0
	nextNonZero := next.Input > 0
	if currentNonZero != nextNonZero {

		return nextNonZero
	}
	if nextNonZero && !NearlyEqual(next.Input, current.Input) {
		return next.Input < current.Input
	}

	return next.Provider < current.Provider
}

// convertModelsDevToRatioData parses models.dev /api.json and converts
// provider pricing metadata into local ratio format.
// models.dev costs are USD per 1M tokens:
//
//	model_ratio = input_cost_per_1M / 2
//	completion_ratio = output_cost / input_cost
//	cache_ratio = cache_read_cost / input_cost
//
// Duplicate model keys across providers are resolved by selecting the
// cheapest non-zero input cost. If only zero-priced candidates exist,
// a zero ratio is kept.
func ConvertModelsDevToRatioData(deps Capabilities, reader io.Reader) (map[string]any, error) {
	var upstreamData map[string]ModelsDevProvider
	if err := deps.Codec.DecodeJson(reader, &upstreamData); err != nil {
		return nil, fmt.Errorf("failed to decode models.dev response: %w", err)
	}
	if len(upstreamData) == 0 {
		return nil, fmt.Errorf("empty models.dev response")
	}

	providers := make([]string, 0, len(upstreamData))
	for provider := range upstreamData {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	selectedCandidates := make(map[string]ModelsDevCandidate)
	for _, provider := range providers {
		providerData := upstreamData[provider]
		if len(providerData.Models) == 0 {
			continue
		}

		modelNames := make([]string, 0, len(providerData.Models))
		for modelName := range providerData.Models {
			modelNames = append(modelNames, modelName)
		}
		sort.Strings(modelNames)

		for _, modelName := range modelNames {
			candidate, ok := BuildModelsDevCandidate(provider, providerData.Models[modelName].Cost)
			if !ok {
				continue
			}
			current, exists := selectedCandidates[modelName]
			if !exists || ShouldReplaceModelsDevCandidate(current, candidate) {
				selectedCandidates[modelName] = candidate
			}
		}
	}

	if len(selectedCandidates) == 0 {
		return nil, fmt.Errorf("no valid models.dev pricing entries found")
	}

	modelRatioMap := make(map[string]any)
	completionRatioMap := make(map[string]any)
	cacheRatioMap := make(map[string]any)

	for modelName, candidate := range selectedCandidates {
		if candidate.Input == 0 {
			modelRatioMap[modelName] = 0.0
			continue
		}

		modelRatio := candidate.Input * float64(500) / modelsDevInputCostRatioBase
		modelRatioMap[modelName] = RoundRatioValue(modelRatio)

		if candidate.Output != nil {
			completionRatio := *candidate.Output / candidate.Input
			completionRatioMap[modelName] = RoundRatioValue(completionRatio)
		}

		if candidate.CacheRead != nil {
			cacheRatio := *candidate.CacheRead / candidate.Input
			cacheRatioMap[modelName] = RoundRatioValue(cacheRatio)
		}
	}

	converted := make(map[string]any)
	if len(modelRatioMap) > 0 {
		converted["model_ratio"] = modelRatioMap
	}
	if len(completionRatioMap) > 0 {
		converted["completion_ratio"] = completionRatioMap
	}
	if len(cacheRatioMap) > 0 {
		converted["cache_ratio"] = cacheRatioMap
	}
	return converted, nil
}

type SyncChannel struct {
	Name string
	Data map[string]any
}

// RatioDecodeError reports an upstream ratio payload that cannot be used;
// Warning is the log text for the operator, errors.Is matches Err.
type RatioDecodeError struct {
	Err     error
	Warning string
}

func (e *RatioDecodeError) Error() string { return e.Err.Error() }
func (e *RatioDecodeError) Unwrap() error { return e.Err }
func DecodeRatioData(deps Capabilities, bodyBytes []byte, isOpenRouter, isModelsDev bool, name string) (map[string]any, error) {
	warning := ""
	warn := func(message string) { warning = message }
	chItem := struct{ Name string }{Name: name}
	if isOpenRouter {
		converted, err := ConvertOpenRouterToRatioData(deps, bytes.NewReader(bodyBytes))
		if err != nil {
			warn("OpenRouter parse failed from " + chItem.Name + ": " + err.Error())
			return nil, &RatioDecodeError{Err: err, Warning: warning}
		}
		return converted, nil
	}

	if isModelsDev {
		converted, err := ConvertModelsDevToRatioData(deps, bytes.NewReader(bodyBytes))
		if err != nil {
			warn("models.dev parse failed from " + chItem.Name + ": " + err.Error())
			return nil, &RatioDecodeError{Err: err, Warning: warning}
		}
		return converted, nil
	}

	// Two upstream response formats are supported:
	//  type1: /api/ratio_config -> data is a map[string]any with model_ratio/completion_ratio/cache_ratio/model_price
	//  type2: /api/pricing      -> data is a []Pricing list converted to the same map format as type1
	var body struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}

	if err := deps.Codec.DecodeJson(bytes.NewReader(bodyBytes), &body); err != nil {
		warn("json decode failed from " + chItem.Name + ": " + err.Error())
		return nil, &RatioDecodeError{Err: err, Warning: warning}
	}

	if !body.Success {
		return nil, &RatioDecodeError{Err: errors.New(body.Message), Warning: warning}
	}

	// Try type1 first
	var type1Data map[string]any
	if err := deps.Codec.Unmarshal(body.Data, &type1Data); err == nil {

		isType1 := false
		for _, rt := range pricingSyncFields {
			if _, ok := type1Data[rt]; ok {
				isType1 = true
				break
			}
		}
		if isType1 {
			return type1Data, nil
		}
	}

	// Otherwise parse type2 (/api/pricing)
	var pricingItems []struct {
		ModelName            string   `json:"model_name"`
		QuotaType            int      `json:"quota_type"`
		ModelRatio           *float64 `json:"model_ratio"`
		ModelPrice           *float64 `json:"model_price"`
		CompletionRatio      *float64 `json:"completion_ratio"`
		CacheRatio           *float64 `json:"cache_ratio"`
		CreateCacheRatio     *float64 `json:"create_cache_ratio"`
		ImageRatio           *float64 `json:"image_ratio"`
		AudioRatio           *float64 `json:"audio_ratio"`
		AudioCompletionRatio *float64 `json:"audio_completion_ratio"`
		BillingMode          string   `json:"billing_mode"`
		BillingExpr          string   `json:"billing_expr"`
	}
	if err := deps.Codec.Unmarshal(body.Data, &pricingItems); err != nil {
		warn("unrecognized data format from " + chItem.Name + ": " + err.Error())
		return nil, &RatioDecodeError{Err: ErrUpstreamDataUnrecognized, Warning: warning}
	}

	modelRatioMap := make(map[string]float64)
	completionRatioMap := make(map[string]float64)
	cacheRatioMap := make(map[string]float64)
	createCacheRatioMap := make(map[string]float64)
	imageRatioMap := make(map[string]float64)
	audioRatioMap := make(map[string]float64)
	audioCompletionRatioMap := make(map[string]float64)
	modelPriceMap := make(map[string]float64)
	billingModeMap := make(map[string]string)
	billingExprMap := make(map[string]string)

	for _, item := range pricingItems {
		if item.ModelName == "" {
			continue
		}
		if item.BillingMode == BillingModeTieredExpr {
			billingModeMap[item.ModelName] = BillingModeTieredExpr
			billingExprMap[item.ModelName] = item.BillingExpr
			continue
		}
		if item.QuotaType == 1 {
			if item.ModelPrice != nil {
				modelPriceMap[item.ModelName] = *item.ModelPrice
			}
		} else {
			if item.ModelRatio != nil {
				modelRatioMap[item.ModelName] = *item.ModelRatio
			}
			if item.CompletionRatio != nil {
				completionRatioMap[item.ModelName] = *item.CompletionRatio
			}
		}
		if item.CacheRatio != nil {
			cacheRatioMap[item.ModelName] = *item.CacheRatio
		}
		if item.CreateCacheRatio != nil {
			createCacheRatioMap[item.ModelName] = *item.CreateCacheRatio
		}
		if item.ImageRatio != nil {
			imageRatioMap[item.ModelName] = *item.ImageRatio
		}
		if item.AudioRatio != nil {
			audioRatioMap[item.ModelName] = *item.AudioRatio
		}
		if item.AudioCompletionRatio != nil {
			audioCompletionRatioMap[item.ModelName] = *item.AudioCompletionRatio
		}
	}

	converted := make(map[string]any)

	if len(modelRatioMap) > 0 {
		ratioAny := make(map[string]any, len(modelRatioMap))
		for k, v := range modelRatioMap {
			ratioAny[k] = v
		}
		converted["model_ratio"] = ratioAny
	}

	if len(completionRatioMap) > 0 {
		compAny := make(map[string]any, len(completionRatioMap))
		for k, v := range completionRatioMap {
			compAny[k] = v
		}
		converted["completion_ratio"] = compAny
	}
	if len(cacheRatioMap) > 0 {
		converted["cache_ratio"] = ValueMap(cacheRatioMap)
	}
	if len(createCacheRatioMap) > 0 {
		converted["create_cache_ratio"] = ValueMap(createCacheRatioMap)
	}
	if len(imageRatioMap) > 0 {
		converted["image_ratio"] = ValueMap(imageRatioMap)
	}
	if len(audioRatioMap) > 0 {
		converted["audio_ratio"] = ValueMap(audioRatioMap)
	}
	if len(audioCompletionRatioMap) > 0 {
		converted["audio_completion_ratio"] = ValueMap(audioCompletionRatioMap)
	}

	if len(modelPriceMap) > 0 {
		priceAny := make(map[string]any, len(modelPriceMap))
		for k, v := range modelPriceMap {
			priceAny[k] = v
		}
		converted["model_price"] = priceAny
	}
	if len(billingModeMap) > 0 {
		converted["billing_mode"] = ValueMap(billingModeMap)
	}
	if len(billingExprMap) > 0 {
		converted["billing_expr"] = ValueMap(billingExprMap)
	}

	return converted, nil
}

// RatioDifference preserves the public ratio comparison fields.
type RatioDifference struct {
	Current    any             `json:"current"`
	Upstreams  map[string]any  `json:"upstreams"`
	Confidence map[string]bool `json:"confidence"`
}
