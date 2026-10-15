package model

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type pricingOperation struct{ operation, main, basis string }

func configuredPricingOperation(channel *Channel, public, upstream string, endpoint constant.EndpointType) pricingOperation {
	if channel.Type == constant.ChannelTypeGemini && (upstream == "lyria-3-clip-preview" || upstream == "lyria-3-pro-preview" || upstream == "lyria-3.5") {
		return pricingOperation{"music_generation", "audio_output", "song"}
	}
	if channel.Type == constant.ChannelTypeGemini && (strings.HasPrefix(upstream, "text-embedding") || strings.HasPrefix(upstream, "embedding") || strings.HasPrefix(upstream, "gemini-embedding")) {
		return pricingOperation{"embeddings", "input", "tokens"}
	}
	if channel.Type == constant.ChannelTypeOpenAI {
		if endpoint == constant.EndpointTypeOpenAI && slices.Contains(common.GetEndpointTypesByChannelType(channel.Type, upstream), constant.EndpointTypeImageGeneration) {
			return pricingOperation{"image_generation", "output", "image_tokens"}
		}
		switch upstream {
		case "text-embedding-3-large", "text-embedding-3-small", "text-embedding-ada-002":
			return pricingOperation{"embeddings", "input", "tokens"}
		case "tts-1", "tts-1-1106", "tts-1-hd", "tts-1-hd-1106":
			basis := "unknown"
			if constant.CountToken && (&dto.AudioRequest{Model: public}).GetTokenCountMeta().TokenType == types.TokenTypeTextNumber {
				basis = "characters"
			}
			return pricingOperation{"speech_generation", "input", basis}
		case "whisper-1":
			basis := "unknown"
			if constant.CountToken && public == upstream {
				basis = "duration"
			}
			return pricingOperation{"transcription", "audio_input", basis}
		case "gpt-4o-transcribe", "gpt-4o-transcribe-diarize", "gpt-4o-mini-transcribe", "gpt-4o-mini-transcribe-2025-03-20", "gpt-4o-mini-transcribe-2025-12-15", "gpt-transcribe":
			return pricingOperation{"transcription", "input", "tokens"}
		case "gpt-4o-mini-tts", "gpt-4o-mini-tts-2025-03-20", "gpt-4o-mini-tts-2025-12-15":
			if constant.CountToken && (&dto.AudioRequest{Model: public}).GetTokenCountMeta().TokenType == types.TokenTypeTokenizer {
				return pricingOperation{"speech_generation", "input", "speech_input_tokens"}
			}
			return pricingOperation{"speech_generation", "input", "unknown"}
		case "gpt-realtime", "gpt-realtime-2025-08-28", "gpt-realtime-1.5", "gpt-realtime-2", "gpt-realtime-2.1", "gpt-realtime-mini", "gpt-realtime-mini-2025-12-15", "gpt-realtime-2.1-mini", "gpt-realtime-translate", "gpt-realtime-whisper":
			return pricingOperation{"realtime", "input", "realtime_tokens"}
		}
	}
	switch endpoint {
	case constant.EndpointTypeEmbeddings:
		return pricingOperation{"embeddings", "input", "tokens"}
	case constant.EndpointTypeJinaRerank:
		return pricingOperation{"rerank", "input", "tokens"}
	case constant.EndpointTypeImageGeneration:
		if channel.Type == constant.ChannelTypeOpenAI {
			return pricingOperation{"image_generation", "output", "image_tokens"}
		}
		return pricingOperation{"image_generation", "image_output", "image"}
	case constant.EndpointTypeOpenAIVideo:
		return pricingOperation{"text_to_video", "video_output", "unknown"}
	case constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini, PricingEndpointBatch:
		return pricingOperation{"text", "output", "tokens"}
	}
	return pricingOperation{}
}

func pricingComponent(variable string) string {
	switch variable {
	case "p":
		return "input"
	case "c":
		return "output"
	case "cr":
		return "cache_read"
	case "cc":
		return "cache_write"
	case "cc1h":
		return "cache_write_1h"
	case "img":
		return "image_input"
	case "img_o":
		return "image_output"
	case "ai":
		return "audio_input"
	case "ao":
		return "audio_output"
	}
	return variable
}

func configuredOperationOffers(pricing Pricing, profile pricingOperation, mode, expression string, public ...bool) []CatalogOffer {
	if profile.operation == "" {
		return nil
	}
	unknown := CatalogOffer{ID: "configured:unknown", Operation: profile.operation, Mode: mode, MainComponent: profile.main, Prices: []CatalogPrice{}}
	if profile.basis == "song" && pricing.BillingMode == billing_setting.BillingModeTieredExpr {
		return []CatalogOffer{unknown}
	}
	formula := pricing.BillingMode == billing_setting.BillingModeTieredExpr || mode == "batch"
	if formula {
		offers := []CatalogOffer{}
		rows := pricing.ConfiguredPrices[expression]
		var publicTiers []billingexpr.CatalogPriceTier
		if len(public) > 0 && public[0] {
			publicTiers = billingexpr.CatalogPriceTiers(expression)
			rows = make([][]billingexpr.DisplayPrice, len(publicTiers))
			for i, tier := range publicTiers {
				rows[i] = tier.Prices
			}
			unknown.PublicConditional = true
		}
		for index, row := range rows {
			offer := unknown
			offer.ID = fmt.Sprintf("configured:%d:%s", index, expression)
			if len(publicTiers) > 0 {
				offer.PublicConditions = publicTiers[index].Conditions
				offer.PublicConditional = len(publicTiers) > 1 || len(offer.PublicConditions) > 0
			}
			fixed := len(row) == 1 && row[0].Component == "base"
			if fixed {
				offer.MainComponent = "base"
			}
			if profile.basis == "image_tokens" && !fixed {
				for _, price := range row {
					if price.Component == "img_o" {
						offer.MainComponent = "image_output"
					}
				}
			}
			if profile.basis == "unknown" && !fixed {
				continue
			}
			valid, outputPayable := true, false
			for _, price := range row {
				component, unit, amount := pricingComponent(price.Component), price.Unit, price.AmountUSD

				if profile.basis == "speech_input_tokens" && !fixed {
					if component == "output" || component == "audio_output" {
						outputPayable = outputPayable || amount != 0
						continue
					}
					if component != "input" {
						valid = false
						break
					}
				}
				if !fixed && profile.basis == "characters" {
					if component != "input" {
						valid = false
						break
					}
					unit = "million_characters"
				}
				if !fixed && profile.basis == "duration" {
					if component != "input" {
						valid = false
						break
					}
					component = "audio_input"
					unit = "minute"
					amount /= 1000
				}
				if profile.operation == "embeddings" && component == "output" {
					if amount != 0 {
						valid = false
					}
					continue
				}
				offer.Prices = append(offer.Prices, CatalogPrice{Component: component, Unit: unit, AmountUSD: &amount})
			}
			if valid {
				if profile.operation == "transcription" && !slices.ContainsFunc(offer.Prices, func(price CatalogPrice) bool { return price.Component == "input" }) && slices.ContainsFunc(offer.Prices, func(price CatalogPrice) bool { return price.Component == "output" }) {
					offer.MainComponent = "output"
				}
				if outputPayable {
					offer.Prices = append(offer.Prices, CatalogPrice{Component: "audio_output", Unit: "unknown"})
				}
				offers = append(offers, offer)
			}
		}
		if len(offers) > 0 {
			return offers
		}
		return []CatalogOffer{unknown}
	}
	if !pricing.PriceConfigured {
		return []CatalogOffer{unknown}
	}
	if pricing.QuotaType == 1 {
		amount := pricing.ModelPrice
		component, unit := "base", "call"
		if profile.operation == "image_generation" {
			component, unit = "image_output", "image"
		} else if profile.basis == "song" {
			component, unit = "audio_output", "song"
		}
		unknown.MainComponent = component
		unknown.Prices = []CatalogPrice{{Component: component, Unit: unit, AmountUSD: &amount}}
		return []CatalogOffer{unknown}
	}
	if profile.basis == "unknown" || profile.basis == "image" || profile.basis == "song" {
		return []CatalogOffer{unknown}
	}
	input := pricing.ModelRatio * 2
	unit, component := "million_tokens", "input"
	if profile.basis == "characters" {
		unit = "million_characters"
	}
	if profile.basis == "duration" {
		unit = "minute"
		component = "audio_input"
		input = pricing.ModelRatio / 500
	}
	unknown.Prices = []CatalogPrice{{Component: component, Unit: unit, AmountUSD: &input}}
	if profile.basis == "speech_input_tokens" {
		audioRatio := ratio_setting.GetAudioRatio(pricing.ModelName)
		if pricing.AudioRatio != nil {
			audioRatio = *pricing.AudioRatio
		}
		audioCompletionRatio := ratio_setting.GetAudioCompletionRatio(pricing.ModelName)
		if pricing.AudioCompletionRatio != nil {
			audioCompletionRatio = *pricing.AudioCompletionRatio
		}
		if pricing.ModelRatio != 0 && (pricing.CompletionRatio != 0 || audioRatio*audioCompletionRatio != 0) {
			unknown.Prices = append(unknown.Prices, CatalogPrice{Component: "audio_output", Unit: "unknown"})
		}
		return []CatalogOffer{unknown}
	}

	if profile.basis != "tokens" && profile.basis != "image_tokens" && profile.basis != "realtime_tokens" {
		return []CatalogOffer{unknown}
	}
	if profile.operation == "text" || profile.operation == "image_generation" || profile.operation == "realtime" || profile.operation == "transcription" {
		output := input * pricing.CompletionRatio
		unknown.Prices = append(unknown.Prices, CatalogPrice{Component: "output", Unit: unit, AmountUSD: &output})
	}
	for _, extra := range []struct {
		component string
		ratio     *float64
	}{{"cache_read", pricing.CacheRatio}, {"cache_write", pricing.CreateCacheRatio}, {"image_input", pricing.ImageRatio}} {
		if extra.ratio != nil && profile.basis != "realtime_tokens" {
			amount := input * *extra.ratio
			unknown.Prices = append(unknown.Prices, CatalogPrice{Component: extra.component, Unit: "million_tokens", AmountUSD: &amount})
		}
	}
	if pricing.AudioRatio != nil || pricing.AudioCompletionRatio != nil || profile.basis == "realtime_tokens" {
		audioInput := input * ratio_setting.GetAudioRatio(pricing.ModelName)
		audioCompletionRatio := ratio_setting.GetAudioCompletionRatio(pricing.ModelName)
		if pricing.AudioRatio != nil {
			audioInput = input * *pricing.AudioRatio
		}
		if pricing.AudioCompletionRatio != nil {
			audioCompletionRatio = *pricing.AudioCompletionRatio
		}
		audioOutput := audioInput * audioCompletionRatio
		unknown.Prices = append(unknown.Prices, CatalogPrice{Component: "audio_input", Unit: "million_tokens", AmountUSD: &audioInput}, CatalogPrice{Component: "audio_output", Unit: "million_tokens", AmountUSD: &audioOutput})
	}
	return []CatalogOffer{unknown}
}

func projectPricingOperations(pricing *Pricing, abilities []AbilityWithChannel, channels map[int]*Channel, generation *jsplugin.RoutingGeneration, advanced map[int]*dto.AdvancedCustomConfig, taskPluginKeys map[int]string) {
	public := *pricing
	public.CatalogOffersByGroup = make(map[string][]CatalogOffer, len(pricing.CatalogOffersByGroup))
	for group, offers := range pricing.CatalogOffersByGroup {
		for _, offer := range offers {
			if len(offer.PublicAlternatives) > 0 {
				public.CatalogOffersByGroup[group] = append(public.CatalogOffersByGroup[group], offer.PublicAlternatives...)
			} else {
				for _, sources := range pricing.BillingSourcesForGroup(group) {
					if slices.Contains(sources, BillingSourceOpenRouterActualCost) {
						offer.Prices = nil
						offer.PublicConditional = true
						break
					}
				}
				public.CatalogOffersByGroup[group] = append(public.CatalogOffersByGroup[group], offer)
			}
		}
	}
	projectPricingOperationsMode(pricing, abilities, channels, generation, advanced, false, taskPluginKeys)
	projectPricingOperationsMode(&public, abilities, channels, generation, advanced, true, taskPluginKeys)
	pricing.PublicCatalogOffersByGroup = public.CatalogOffersByGroup
}

func projectPricingOperationsMode(pricing *Pricing, abilities []AbilityWithChannel, channels map[int]*Channel, generation *jsplugin.RoutingGeneration, advanced map[int]*dto.AdvancedCustomConfig, public bool, taskPluginKeys map[int]string) {
	native := pricing.CatalogOffersByGroup
	pricing.CatalogOffersByGroup = make(map[string][]CatalogOffer)
	pricing.ConsumerCanonicalModelsByGroup = make(map[string]string)
	unpairedGroups := make(map[string]bool)
	for _, ability := range abilities {
		if ability.Model != pricing.ModelName {
			continue
		}
		channel := channels[ability.ChannelId]
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		upstream, resolved := ResolvePricingUpstreamModel(channel, ability.Model)
		if !resolved {
			continue
		}
		prefix := fmt.Sprintf("%d:%s:", channel.Id, upstream)
		var offers []CatalogOffer
		for _, offer := range native[ability.Group] {
			if strings.HasPrefix(offer.ID, prefix) {
				offers = append(offers, offer)
			}
		}
		legacy, video, _ := legacyVideoPriceBasis(generation, channel, ability.Model, taskPluginKeys[channel.Id])
		actual := channel.Type == constant.ChannelTypeOpenRouter || channel.Type == constant.ChannelTypeTaskPlugin && taskPluginKeys[channel.Id] == "openrouter-video"
		batchAlias := strings.HasSuffix(ability.Model, ":batch")
		if batchAlias && actual {
			base, paired := pairedPricingBatchBase(ability, upstream, abilities, channels)
			if paired {
				pricing.ConsumerCanonicalModelsByGroup[ability.Group] = base
			} else {
				unpairedGroups[ability.Group] = true
			}
			batchOffers := []CatalogOffer{}
			for _, offer := range offers {
				if offer.Mode == "batch" {
					batchOffers = append(batchOffers, offer)
				}
			}
			if len(batchOffers) == 0 {
				batchOffers = append(batchOffers, CatalogOffer{ID: prefix + "batch:unknown", Operation: "text", Mode: "batch", MainComponent: "output"})
			}
			pricing.CatalogOffersByGroup[ability.Group] = append(pricing.CatalogOffersByGroup[ability.Group], batchOffers...)
			continue
		}
		if video {
			if !actual && pricing.BillingMode == billing_setting.BillingModeTieredExpr && channel.Type == constant.ChannelTypeGemini && strings.HasPrefix(upstream, "veo-") {
				offers = append(offers, configuredVeoUsageOffers(*pricing, channel, upstream, generation)...)
			}
			if !actual && len(legacy) > 0 {
				for action, basis := range legacy {
					profile := pricingOperation{action, "video_output", "unknown"}
					if pricing.QuotaType == 1 && pricing.PriceConfigured && pricing.BillingMode != billing_setting.BillingModeTieredExpr {
						amount := pricing.ModelPrice
						unit := basis.Unit
						if slices.Contains(constant.TaskPricePatches, pricing.ModelName) {
							unit = "call"
						}
						offers = append(offers, CatalogOffer{ID: prefix + action, Operation: action, Mode: "sync", MainComponent: "video_output", Prices: []CatalogPrice{{Component: "video_output", Unit: unit, AmountUSD: &amount}}})
					} else {
						offers = append(offers, configuredOperationOffers(*pricing, profile, "sync", pricing.BillingExpr, public)...)
					}
				}
			}
			pricing.CatalogOffersByGroup[ability.Group] = append(pricing.CatalogOffersByGroup[ability.Group], offers...)
			continue
		}
		if actual {
			offers = imagePricingWithTokenInputs(offers)
			if len(offers) == 0 {
				if source, known := GetPricingCatalogEntry(channel, upstream, constant.EndpointTypeOpenAI); known && len(source.Offers) == 0 {
					continue
				}
				operation, main := "text", "output"
				if slices.Contains(pricing.SupportedEndpointTypes, constant.EndpointTypeImageGeneration) {
					operation, main = "image_generation", "image_output"
				}
				offers = append(offers, CatalogOffer{ID: prefix + "unknown", Operation: operation, Mode: "sync", MainComponent: main})
			}
			pricing.CatalogOffersByGroup[ability.Group] = append(pricing.CatalogOffersByGroup[ability.Group], offers...)
			continue
		}
		endpoints := getPricingEndpointTypesForAbility(ability, advanced)
		if slices.Contains(endpoints, constant.EndpointTypeImageGeneration) {
			endpoints = []constant.EndpointType{constant.EndpointTypeImageGeneration}
		}
		for _, endpoint := range endpoints {
			profile := configuredPricingOperation(channel, ability.Model, upstream, endpoint)
			offers = append(offers, configuredOperationOffers(*pricing, profile, "sync", pricing.BillingExpr, public)...)
			batchSources := pricing.BillingSourcesForGroup(ability.Group)[PricingEndpointBatch]
			if expression, known := billing_setting.GetBatchBillingExpr(ability.Model); known && expression != "" && slices.Contains(batchSources, BillingSourceConfiguredTariff) && (profile.operation == "text" || profile.operation == "embeddings") {
				offers = append(offers, configuredOperationOffers(*pricing, profile, "batch", expression, public)...)
			}
		}
		pricing.CatalogOffersByGroup[ability.Group] = append(pricing.CatalogOffersByGroup[ability.Group], offers...)
	}
	for group := range unpairedGroups {
		delete(pricing.ConsumerCanonicalModelsByGroup, group)
	}
}

func pairedPricingBatchBase(ability AbilityWithChannel, upstream string, abilities []AbilityWithChannel, channels map[int]*Channel) (string, bool) {
	if ability.ChannelType != constant.ChannelTypeOpenRouter || !strings.HasSuffix(upstream, ":batch") {
		return "", false
	}
	if expression, known := billing_setting.GetBatchBillingExpr(ability.Model); !known || expression == "" {
		return "", false
	}
	base := strings.TrimSuffix(ability.Model, ":batch")
	for _, candidate := range abilities {
		if candidate.ChannelId != ability.ChannelId || candidate.Group != ability.Group || candidate.Model != base {
			continue
		}
		mapped, known := ResolvePricingUpstreamModel(channels[candidate.ChannelId], candidate.Model)
		if known && mapped == strings.TrimSuffix(upstream, ":batch") {
			return base, true
		}
	}
	return "", false
}

func ConsumerPricingCatalogue(pricing []Pricing, usableGroups map[string]string) []Pricing {
	byName := make(map[string]int, len(pricing))
	for index, item := range pricing {
		byName[item.ModelName] = index
	}
	result := []Pricing{}
	positions := make(map[int]int)
	groups := slices.Sorted(maps.Keys(usableGroups))
	for sourceIndex, item := range pricing {
		for _, group := range groups {
			if !slices.Contains(item.EnableGroup, group) && !slices.Contains(item.EnableGroup, "all") {
				continue
			}
			canonical := item.ConsumerCanonicalModelsByGroup[group]
			if canonical == "" && slices.Contains(item.EnableGroup, "all") {
				canonical = item.ConsumerCanonicalModelsByGroup["all"]
			}
			if canonical == "" {
				canonical = item.ModelName
			}
			baseIndex := sourceIndex
			if canonical != item.ModelName {
				if targetIndex, exists := byName[canonical]; exists {
					baseIndex = targetIndex
				}
			}
			base := pricing[baseIndex]
			index, exists := positions[baseIndex]
			if !exists {
				index = len(result)
				positions[baseIndex] = index
				base.EnableGroup = []string{}
				base.CatalogOffersByGroup = make(map[string][]CatalogOffer)
				base.PublicCatalogOffersByGroup = make(map[string][]CatalogOffer)
				result = append(result, base)
			}
			if !slices.Contains(result[index].EnableGroup, group) {
				result[index].EnableGroup = append(result[index].EnableGroup, group)
			}
			result[index].CatalogOffersByGroup[group] = append(result[index].CatalogOffersByGroup[group], item.CatalogOffersForGroup(group)...)
			result[index].PublicCatalogOffersByGroup[group] = append(result[index].PublicCatalogOffersByGroup[group], item.PublicCatalogOffersByGroup["all"]...)
			result[index].PublicCatalogOffersByGroup[group] = append(result[index].PublicCatalogOffersByGroup[group], item.PublicCatalogOffersByGroup[group]...)
		}
	}
	return result
}

func catalogOfferProviderKey(id string) string {
	last := strings.LastIndex(id, ":")
	if last < 0 {
		return ""
	}
	previous := strings.LastIndex(id[:last], ":")
	if previous < 0 {
		return ""
	}
	if _, err := strconv.ParseUint(id[previous+1:last], 10, 64); err != nil {
		return ""
	}
	key := id[:previous]
	if strings.Count(key, ":") < 2 || strings.HasSuffix(key, ":") {
		return ""
	}
	return key
}

func imagePricingWithTokenInputs(offers []CatalogOffer) []CatalogOffer {
	result := slices.Clone(offers)
	for index, offer := range offers {
		if offer.Operation != "image_generation" {
			continue
		}
		images := offer.PublicAlternatives
		if len(images) == 0 {
			images = []CatalogOffer{offer}
		}
		combined := []CatalogOffer{}
		for _, image := range images {
			provider := catalogOfferProviderKey(image.ID)
			matched := false
			for _, tokenOffer := range offers {
				if tokenOffer.Operation != "text" || tokenOffer.Mode != image.Mode {
					continue
				}
				tokens := tokenOffer.PublicAlternatives
				if len(tokens) == 0 {
					tokens = []CatalogOffer{tokenOffer}
				}
				for _, token := range tokens {
					if provider == "" || catalogOfferProviderKey(token.ID) != provider {
						continue
					}
					merged := image
					merged.Prices = slices.Clone(image.Prices)
					merged.PublicAlternatives = nil
					added := false
					for _, price := range token.Prices {
						if !slices.Contains([]string{"input", "cache_read", "cache_write", "cache_write_1h"}, price.Component) {
							continue
						}
						if slices.ContainsFunc(merged.Prices, func(existing CatalogPrice) bool { return existing.Component == price.Component }) {
							continue
						}
						merged.Prices = append(merged.Prices, price)
						added = true
					}
					if !added {
						continue
					}
					merged.ID = image.ID + ":tokens:" + token.ID
					merged.PublicConditions = append(slices.Clone(image.PublicConditions), token.PublicConditions...)
					merged.PublicConditional = image.PublicConditional || token.PublicConditional
					combined = append(combined, merged)
					matched = true
				}
			}
			if !matched {
				combined = append(combined, image)
			}
		}
		if selected := SelectCatalogOffer(combined); selected != nil {
			result[index] = *selected
			result[index].PublicAlternatives = combined
		}
	}
	return result
}
