package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	catalogdomain "github.com/QuantumNous/new-api/internal/catalog/domain"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"math"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"maps"
	"slices"
	"strconv"
	"strings"
)

type BuildModel = catalogdomain.Model
type BuildVendor = catalogdomain.Vendor
type BuildChannel channeldomain.Channel

func (c *BuildChannel) GetBaseURL() string {
	if c.BaseURL == nil {
		return ""
	}
	url := *c.BaseURL
	if url == "" {
		url = constant.GetChannelBaseURL(c.Type)
	}
	return url
}
func (c *BuildChannel) GetModelMapping() string {
	if c.ModelMapping == nil {
		return ""
	}
	return *c.ModelMapping
}
func (c *BuildChannel) GetModels() []string {
	if c.Models == "" {
		return []string{}
	}
	return strings.Split(strings.Trim(c.Models, ","), ",")
}

type BuildAbility struct {
	Model, Group           string
	ChannelId, ChannelType int
}

func LegacyVideoPriceBasis(generation PluginGeneration, channel *BuildChannel, modelName, taskPluginKey string) (map[string]jsplugin.LegacyPriceBasis, bool, bool) {
	if channel == nil {
		return nil, false, false
	}
	declared, known := generation.CanonicalModel(modelName)
	if !known {
		mapping := make(map[string]string)
		if err := common.UnmarshalJsonStr(channel.GetModelMapping(), &mapping); err != nil {
			return nil, false, false
		}
		tail, cyclic := FollowChannelModelMapping(mapping, modelName)
		if cyclic {
			return nil, false, false
		}
		declared, known = generation.CanonicalModel(tail)
		if !known {
			return nil, false, false
		}
	}
	for _, candidate := range generation.LookupEndpointCandidates("POST", "/v1/videos", declared) {
		if candidate.Plugin == nil {
			continue
		}
		meta := candidate.Plugin.Meta
		if channel.Type == constant.ChannelTypeTaskPlugin {
			if taskPluginKey != meta.Key {
				continue
			}
		} else if !slices.Contains(meta.ChannelTypes, channel.Type) {
			continue
		}
		return maps.Clone(meta.LegacyPriceBasis), true, len(meta.Protocols) == 1 && meta.Protocols[0].Name == "openai_video"
	}
	return nil, false, false
}
func GetPricingEndpointTypesForAbility(ability BuildAbility, advancedCustomConfigs map[int]*dto.AdvancedCustomConfig) []constant.EndpointType {
	if ability.ChannelType != constant.ChannelTypeAdvancedCustom {
		endpoints := common.GetEndpointTypesByChannelType(ability.ChannelType, ability.Model)
		return endpoints
	}
	if config := advancedCustomConfigs[ability.ChannelId]; config != nil {
		return config.SupportedEndpointTypesForModel(ability.Model)
	}
	return common.GetEndpointTypesByChannelType(ability.ChannelType, ability.Model)
}
func AppendPricingEndpoint(endpoints []string, endpoint string) []string {
	if endpoint == "" || common.StringsContains(endpoints, endpoint) {
		return endpoints
	}
	return append(endpoints, endpoint)
}

// Catalog semantics describe the consumer operation, independently of the relay
// protocol. In particular, an image Chat request is not a text-generation offer.
func CatalogConsumerProfile(document *catalogsync.Document) (PricingOperation, string) {
	if document == nil {
		return PricingOperation{}, ""
	}
	output := document.Modalities.Output
	switch document.Type {
	case "text":
		if slices.Contains(output, "text") {
			return PricingOperation{"text", "output", "tokens"}, "TEXT"
		}
	case "image":
		if slices.Contains(output, "image") {
			return PricingOperation{"image_generation", "image_output", "image"}, "IMAGE"
		}
	case "video":
		if slices.Contains(output, "video") {
			return PricingOperation{"text_to_video", "video_output", "unknown"}, "VIDEO"
		}
	case "embedding":
		if slices.Contains(output, "embeddings") {
			return PricingOperation{"embeddings", "input", "tokens"}, "EMBEDDINGS"
		}
	case "rerank":
		if slices.Contains(output, "rerank") {
			return PricingOperation{"rerank", "input", "tokens"}, "RERANK"
		}
	case "speech":
		if slices.Contains(output, "speech") {
			return PricingOperation{"speech_generation", "audio_output", "unknown"}, "OTHER"
		}
	case "transcription":
		if slices.Contains(output, "transcription") {
			return PricingOperation{"transcription", "audio_input", "unknown"}, "OTHER"
		}
	case "audio":
		if slices.Contains(output, "audio") {
			return PricingOperation{"audio_generation", "audio_output", "unknown"}, "OTHER"
		}
	case "decision":
		if slices.Contains(output, "decisions") {
			return PricingOperation{"decisions", "input", "tokens"}, "TEXT"
		}
		return PricingOperation{}, "TEXT"
	}
	return PricingOperation{}, "OTHER"
}
func CatalogConsumerOperation(document *catalogsync.Document, upstream string, endpoint constant.EndpointType) PricingOperation {
	profile, _ := CatalogConsumerProfile(document)
	if profile.Operation == "" {
		return PricingOperation{}
	}
	batch := strings.HasSuffix(upstream, ":batch")
	if profile.Operation == "decisions" {
		if endpoint == PricingEndpointSystemOne && !batch {
			return profile
		}
		return PricingOperation{}
	}
	if batch && endpoint != PricingEndpointBatch {
		return PricingOperation{}
	}
	if endpoint == PricingEndpointBatch && !batch && !slices.ContainsFunc(document.Offers, func(offer catalogsync.Offer) bool {
		return offer.Mode != nil && *offer.Mode == "batch"
	}) {
		return PricingOperation{}
	}
	switch endpoint {
	case PricingEndpointBatch:
		if profile.Operation == "text" || profile.Operation == "embeddings" {
			return profile
		}
	case constant.EndpointTypeEmbeddings:
		if profile.Operation == "embeddings" {
			return profile
		}
	case constant.EndpointTypeJinaRerank:
		if profile.Operation == "rerank" {
			return profile
		}
	case constant.EndpointTypeImageGeneration:
		if profile.Operation == "image_generation" {
			return profile
		}
	case constant.EndpointTypeOpenAIVideo:
		if profile.Operation == "text_to_video" {
			return profile
		}
	case constant.EndpointTypeOpenAI:
		if profile.Operation != "embeddings" && profile.Operation != "text_to_video" {
			return profile
		}
	case constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini:
		if profile.Operation == "text" {
			return profile
		}
	}
	return PricingOperation{}
}

// Only actual OpenRouter channels can consume the initial acquisition source.
// A compatible protocol or an identical model name does not establish identity.
func IsOpenRouterCatalogChannel(channel *BuildChannel) bool {
	if channel == nil || channel.Type != constant.ChannelTypeOpenRouter {
		return false
	}
	base := channel.GetBaseURL()
	if base == "" {
		base = constant.GetChannelBaseURL(channel.Type)
	}
	return strings.TrimRight(base, "/") == "https://openrouter.ai/api"
}

// followChannelModelMapping walks one channel's mapping the same way
// ModelMappedHelper does: visited-set cycle detection, self-map stops at
// the current hop, a non-self cycle is reported to the caller.
func FollowChannelModelMapping(modelMap map[string]string, start string) (string, bool) {
	current := start
	visited := map[string]bool{current: true}
	for {
		mapped, exists := modelMap[current]
		if !exists || mapped == "" {
			return current, false
		}
		if visited[mapped] {
			if mapped == current {
				return current, false
			}
			return "", true
		}
		visited[mapped] = true
		current = mapped
	}
}
func ResolvePricingUpstreamModel(channel *BuildChannel, modelName string) (string, bool) {
	if channel == nil || strings.TrimSpace(modelName) == "" {
		return "", false
	}
	if strings.TrimSpace(channel.GetModelMapping()) == "" {
		return modelName, true
	}
	var mapping map[string]string
	if common.UnmarshalJsonStr(channel.GetModelMapping(), &mapping) != nil {
		return "", false
	}
	upstream, cyclic := FollowChannelModelMapping(mapping, modelName)
	return upstream, !cyclic
}

type PricingOperation struct {
	Operation, Main, Basis string `json:"-"`
}

func ConfiguredPricingOperation(channel *BuildChannel, public, upstream string, endpoint constant.EndpointType) PricingOperation {
	if channel.Type == constant.ChannelTypeGemini && (upstream == "lyria-3-clip-preview" || upstream == "lyria-3-pro-preview" || upstream == "lyria-3.5") {
		return PricingOperation{"music_generation", "audio_output", "song"}
	}
	if channel.Type == constant.ChannelTypeGemini && (strings.HasPrefix(upstream, "text-embedding") || strings.HasPrefix(upstream, "embedding") || strings.HasPrefix(upstream, "gemini-embedding")) {
		return PricingOperation{"embeddings", "input", "tokens"}
	}
	if channel.Type == constant.ChannelTypeOpenAI {
		if endpoint == constant.EndpointTypeOpenAI && slices.Contains(common.GetEndpointTypesByChannelType(channel.Type, upstream), constant.EndpointTypeImageGeneration) {
			return PricingOperation{"image_generation", "output", "image_tokens"}
		}
		switch upstream {
		case "text-embedding-3-large", "text-embedding-3-small", "text-embedding-ada-002":
			return PricingOperation{"embeddings", "input", "tokens"}
		case "tts-1", "tts-1-1106", "tts-1-hd", "tts-1-hd-1106":
			basis := "unknown"
			if constant.CountToken && (&dto.AudioRequest{Model: public}).GetTokenCountMeta().TokenType == types.TokenTypeTextNumber {
				basis = "characters"
			}
			return PricingOperation{"speech_generation", "input", basis}
		case "whisper-1":
			basis := "unknown"
			if constant.CountToken && public == upstream {
				basis = "duration"
			}
			return PricingOperation{"transcription", "audio_input", basis}
		case "gpt-4o-transcribe", "gpt-4o-transcribe-diarize", "gpt-4o-mini-transcribe", "gpt-4o-mini-transcribe-2025-03-20", "gpt-4o-mini-transcribe-2025-12-15", "gpt-transcribe":
			return PricingOperation{"transcription", "input", "tokens"}
		case "gpt-4o-mini-tts", "gpt-4o-mini-tts-2025-03-20", "gpt-4o-mini-tts-2025-12-15":
			if constant.CountToken && (&dto.AudioRequest{Model: public}).GetTokenCountMeta().TokenType == types.TokenTypeTokenizer {
				return PricingOperation{"speech_generation", "input", "speech_input_tokens"}
			}
			return PricingOperation{"speech_generation", "input", "unknown"}
		case "gpt-realtime", "gpt-realtime-2025-08-28", "gpt-realtime-1.5", "gpt-realtime-2", "gpt-realtime-2.1", "gpt-realtime-mini", "gpt-realtime-mini-2025-12-15", "gpt-realtime-2.1-mini", "gpt-realtime-translate", "gpt-realtime-whisper":
			return PricingOperation{"realtime", "input", "realtime_tokens"}
		}
	}
	switch endpoint {
	case constant.EndpointTypeEmbeddings:
		return PricingOperation{"embeddings", "input", "tokens"}
	case constant.EndpointTypeJinaRerank:
		return PricingOperation{"rerank", "input", "tokens"}
	case constant.EndpointTypeImageGeneration:
		if channel.Type == constant.ChannelTypeOpenAI {
			return PricingOperation{"image_generation", "output", "image_tokens"}
		}
		return PricingOperation{"image_generation", "image_output", "image"}
	case constant.EndpointTypeOpenAIVideo:
		return PricingOperation{"text_to_video", "video_output", "unknown"}
	case constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini, PricingEndpointBatch:
		return PricingOperation{"text", "output", "tokens"}
	}
	return PricingOperation{}
}
func PricingComponent(variable string) string {
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
func (pricingPolicy *Operations) ConfiguredOperationOffers(pricing Pricing, profile PricingOperation, mode, expression string, public ...bool) []CatalogOffer {
	if profile.Operation == "" {
		return nil
	}
	unknown := CatalogOffer{ID: "configured:unknown", Operation: profile.Operation, Mode: mode, MainComponent: profile.Main, Prices: []CatalogPrice{}}
	if profile.Basis == "song" && pricing.BillingMode == BillingModeTieredExpr {
		return []CatalogOffer{unknown}
	}
	formula := pricing.BillingMode == BillingModeTieredExpr || mode == "batch"
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
			if profile.Basis == "image_tokens" && !fixed {
				for _, price := range row {
					if price.Component == "img_o" {
						offer.MainComponent = "image_output"
					}
				}
			}
			if profile.Basis == "unknown" && !fixed {
				continue
			}
			valid, outputPayable := true, false
			for _, price := range row {
				component, unit, amount := PricingComponent(price.Component), price.Unit, price.AmountUSD

				if profile.Basis == "speech_input_tokens" && !fixed {
					if component == "output" || component == "audio_output" {
						outputPayable = outputPayable || amount != 0
						continue
					}
					if component != "input" {
						valid = false
						break
					}
				}
				if !fixed && profile.Basis == "characters" {
					if component != "input" {
						valid = false
						break
					}
					unit = "million_characters"
				}
				if !fixed && profile.Basis == "duration" {
					if component != "input" {
						valid = false
						break
					}
					component = "audio_input"
					unit = "minute"
					amount /= 1000
				}
				if profile.Operation == "embeddings" && component == "output" {
					if amount != 0 {
						valid = false
					}
					continue
				}
				offer.Prices = append(offer.Prices, CatalogPrice{Component: component, Unit: unit, AmountUSD: &amount})
			}
			if valid {
				if profile.Operation == "transcription" && !slices.ContainsFunc(offer.Prices, func(price CatalogPrice) bool { return price.Component == "input" }) && slices.ContainsFunc(offer.Prices, func(price CatalogPrice) bool { return price.Component == "output" }) {
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
		if profile.Operation == "image_generation" {
			component, unit = "image_output", "image"
		} else if profile.Basis == "song" {
			component, unit = "audio_output", "song"
		}
		unknown.MainComponent = component
		unknown.Prices = []CatalogPrice{{Component: component, Unit: unit, AmountUSD: &amount}}
		return []CatalogOffer{unknown}
	}
	if profile.Basis == "unknown" || profile.Basis == "image" || profile.Basis == "song" {
		return []CatalogOffer{unknown}
	}
	input := pricing.ModelRatio * 2
	unit, component := "million_tokens", "input"
	if profile.Basis == "characters" {
		unit = "million_characters"
	}
	if profile.Basis == "duration" {
		unit = "minute"
		component = "audio_input"
		input = pricing.ModelRatio / 500
	}
	unknown.Prices = []CatalogPrice{{Component: component, Unit: unit, AmountUSD: &input}}
	if profile.Basis == "speech_input_tokens" {
		audioRatio := pricingPolicy.runtime.AudioRatio(pricing.ModelName)
		if pricing.AudioRatio != nil {
			audioRatio = *pricing.AudioRatio
		}
		audioCompletionRatio := pricingPolicy.runtime.AudioCompletionRatio(pricing.ModelName)
		if pricing.AudioCompletionRatio != nil {
			audioCompletionRatio = *pricing.AudioCompletionRatio
		}
		if pricing.ModelRatio != 0 && (pricing.CompletionRatio != 0 || audioRatio*audioCompletionRatio != 0) {
			unknown.Prices = append(unknown.Prices, CatalogPrice{Component: "audio_output", Unit: "unknown"})
		}
		return []CatalogOffer{unknown}
	}

	if profile.Basis != "tokens" && profile.Basis != "image_tokens" && profile.Basis != "realtime_tokens" {
		return []CatalogOffer{unknown}
	}
	if profile.Operation == "text" || profile.Operation == "image_generation" || profile.Operation == "realtime" || profile.Operation == "transcription" {
		output := input * pricing.CompletionRatio
		unknown.Prices = append(unknown.Prices, CatalogPrice{Component: "output", Unit: unit, AmountUSD: &output})
	}
	for _, extra := range []struct {
		component string
		ratio     *float64
	}{{"cache_read", pricing.CacheRatio}, {"cache_write", pricing.CreateCacheRatio}, {"image_input", pricing.ImageRatio}} {
		if extra.ratio != nil && profile.Basis != "realtime_tokens" {
			amount := input * *extra.ratio
			unknown.Prices = append(unknown.Prices, CatalogPrice{Component: extra.component, Unit: "million_tokens", AmountUSD: &amount})
		}
	}
	if pricing.AudioRatio != nil || pricing.AudioCompletionRatio != nil || profile.Basis == "realtime_tokens" {
		audioInput := input * pricingPolicy.runtime.AudioRatio(pricing.ModelName)
		audioCompletionRatio := pricingPolicy.runtime.AudioCompletionRatio(pricing.ModelName)
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
func (pricingPolicy *Operations) ProjectPricingOperations(pricing *Pricing, abilities []BuildAbility, channels map[int]*BuildChannel, generation PluginGeneration, advanced map[int]*dto.AdvancedCustomConfig, taskPluginKeys map[int]string) {
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
	pricingPolicy.ProjectPricingOperationsMode(pricing, abilities, channels, generation, advanced, false, taskPluginKeys)
	pricingPolicy.ProjectPricingOperationsMode(&public, abilities, channels, generation, advanced, true, taskPluginKeys)
	pricing.PublicCatalogOffersByGroup = public.CatalogOffersByGroup
}
func (pricingPolicy *Operations) ProjectPricingOperationsMode(pricing *Pricing, abilities []BuildAbility, channels map[int]*BuildChannel, generation PluginGeneration, advanced map[int]*dto.AdvancedCustomConfig, public bool, taskPluginKeys map[int]string) {
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
		legacy, video, _ := LegacyVideoPriceBasis(generation, channel, ability.Model, taskPluginKeys[channel.Id])
		actual := channel.Type == constant.ChannelTypeOpenRouter || channel.Type == constant.ChannelTypeTaskPlugin && taskPluginKeys[channel.Id] == "openrouter-video"
		batchAlias := strings.HasSuffix(ability.Model, ":batch")
		if batchAlias && actual {
			base, paired := pricingPolicy.PairedPricingBatchBase(ability, upstream, abilities, channels)
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
			pricing.CatalogOffersByGroup[ability.Group] = append(pricing.CatalogOffersByGroup[ability.Group], batchOffers...)
			continue
		}
		if actual && pricing.CatalogType == "decision" {

			profile := PricingOperation{"text", "output", "tokens"}
			if slices.Contains(pricing.BillingSourcesForGroup(ability.Group)[constant.EndpointTypeOpenAI], BillingSourceConfiguredTariff) {
				offers = append(offers, pricingPolicy.ConfiguredOperationOffers(*pricing, profile, "sync", pricing.BillingExpr, public)...)
			}
			if slices.ContainsFunc(pricing.UpstreamCatalogRoutesForGroup(ability.Group), func(route UpstreamCatalogRoute) bool {
				return route.ChannelID == channel.Id && route.Endpoint == PricingEndpointSystemOne && route.Operation == "decisions"
			}) {
				offers = append(offers, CatalogOffer{ID: prefix + "decisions:unknown", Operation: "decisions", Mode: "sync", MainComponent: "input"})
			}
			if len(offers) == 0 {
				offers = []CatalogOffer{{ID: prefix + "decision:unknown", Operation: "text", Mode: "sync", MainComponent: "output"}}
			}
			pricing.CatalogOffersByGroup[ability.Group] = append(pricing.CatalogOffersByGroup[ability.Group], offers...)
			continue
		}
		if actual && pricing.CatalogModality != "" {
			seen := make(map[string]bool)
			for _, route := range pricing.UpstreamCatalogRoutesForGroup(ability.Group) {
				key := route.Operation + ":" + route.Mode
				if route.ChannelID != channel.Id || route.Mode == "batch" || seen[key] {
					continue
				}
				seen[key] = true
				offers = append(offers, CatalogOffer{ID: prefix + key + ":unknown", Operation: route.Operation, Mode: route.Mode})
			}
			pricing.CatalogOffersByGroup[ability.Group] = append(pricing.CatalogOffersByGroup[ability.Group], offers...)
			continue
		}
		if video {
			if !actual && pricing.BillingMode == BillingModeTieredExpr && channel.Type == constant.ChannelTypeGemini && strings.HasPrefix(upstream, "veo-") {
				offers = append(offers, ConfiguredVeoUsageOffers(*pricing, channel, upstream, generation)...)
			}
			if !actual && len(legacy) > 0 {
				for action, basis := range legacy {
					profile := PricingOperation{action, "video_output", "unknown"}
					if pricing.QuotaType == 1 && pricing.PriceConfigured && pricing.BillingMode != BillingModeTieredExpr {
						amount := pricing.ModelPrice
						unit := basis.Unit
						if slices.Contains(constant.TaskPricePatches, pricing.ModelName) {
							unit = "call"
						}
						offers = append(offers, CatalogOffer{ID: prefix + action, Operation: action, Mode: "sync", MainComponent: "video_output", Prices: []CatalogPrice{{Component: "video_output", Unit: unit, AmountUSD: &amount}}})
					} else {
						offers = append(offers, pricingPolicy.ConfiguredOperationOffers(*pricing, profile, "sync", pricing.BillingExpr, public)...)
					}
				}
			}
			pricing.CatalogOffersByGroup[ability.Group] = append(pricing.CatalogOffersByGroup[ability.Group], offers...)
			continue
		}
		if actual {
			offers = ImagePricingWithTokenInputs(offers)
			if len(offers) == 0 {
				if source, known := pricingPolicy.runtime.CatalogEntry(channel, upstream, constant.EndpointTypeOpenAI); known && len(source.Offers) == 0 {
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
		endpoints := GetPricingEndpointTypesForAbility(ability, advanced)
		if slices.Contains(endpoints, constant.EndpointTypeImageGeneration) {
			endpoints = []constant.EndpointType{constant.EndpointTypeImageGeneration}
		}
		for _, endpoint := range endpoints {
			profile := ConfiguredPricingOperation(channel, ability.Model, upstream, endpoint)
			offers = append(offers, pricingPolicy.ConfiguredOperationOffers(*pricing, profile, "sync", pricing.BillingExpr, public)...)
			batchSources := pricing.BillingSourcesForGroup(ability.Group)[PricingEndpointBatch]
			if expression, known := pricingPolicy.runtime.BatchExpression(ability.Model); known && expression != "" && slices.Contains(batchSources, BillingSourceConfiguredTariff) && (profile.Operation == "text" || profile.Operation == "embeddings") {
				offers = append(offers, pricingPolicy.ConfiguredOperationOffers(*pricing, profile, "batch", expression, public)...)
			}
		}
		pricing.CatalogOffersByGroup[ability.Group] = append(pricing.CatalogOffersByGroup[ability.Group], offers...)
	}
	for group := range unpairedGroups {
		delete(pricing.ConsumerCanonicalModelsByGroup, group)
	}
}
func (pricingPolicy *Operations) PairedPricingBatchBase(ability BuildAbility, upstream string, abilities []BuildAbility, channels map[int]*BuildChannel) (string, bool) {
	if ability.ChannelType != constant.ChannelTypeOpenRouter || !strings.HasSuffix(upstream, ":batch") {
		return "", false
	}
	if expression, known := pricingPolicy.runtime.BatchExpression(ability.Model); !known || expression == "" {
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
func CatalogOfferProviderKey(id string) string {
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
func ImagePricingWithTokenInputs(offers []CatalogOffer) []CatalogOffer {
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
			provider := CatalogOfferProviderKey(image.ID)
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
					if provider == "" || CatalogOfferProviderKey(token.ID) != provider {
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

// 简化的供应商映射规则
var defaultVendorRules = map[string]string{
	"gpt":      "OpenAI",
	"dall-e":   "OpenAI",
	"whisper":  "OpenAI",
	"o1":       "OpenAI",
	"o3":       "OpenAI",
	"claude":   "Anthropic",
	"gemini":   "Google",
	"moonshot": "Moonshot",
	"kimi":     "Moonshot",
	"chatglm":  "Zhipu AI",
	"glm-":     "Zhipu AI",
	"qwen":     "Alibaba",
	"deepseek": "DeepSeek",
	"abab":     "MiniMax",
	"minimax":  "MiniMax",
	"ernie":    "Baidu",
	"spark":    "iFlytek",
	"hunyuan":  "Tencent",
	"command":  "Cohere",
	"@cf/":     "Cloudflare",
	"360":      "360",
	"yi":       "01.AI",
	"jina":     "Jina",
	"mistral":  "Mistral",
	"grok":     "xAI",
	"llama":    "Meta",
	"doubao":   "ByteDance",
	"kling":    "Kuaishou",
	"jimeng":   "Jimeng",
	"vidu":     "Vidu",
}

// 供应商默认图标映射
var defaultVendorIcons = map[string]string{
	"OpenAI":     "OpenAI",
	"Anthropic":  "Claude.Color",
	"Google":     "Gemini.Color",
	"Moonshot":   "Moonshot",
	"Zhipu AI":   "Zhipu.Color",
	"Alibaba":    "Qwen.Color",
	"DeepSeek":   "DeepSeek.Color",
	"MiniMax":    "Minimax.Color",
	"Baidu":      "Wenxin.Color",
	"iFlytek":    "Spark.Color",
	"Tencent":    "Hunyuan.Color",
	"Cohere":     "Cohere.Color",
	"Cloudflare": "Cloudflare.Color",
	"360":        "Ai360.Color",
	"01.AI":      "Yi.Color",
	"Jina":       "Jina",
	"Mistral":    "Mistral.Color",
	"xAI":        "XAI",
	"Meta":       "Ollama",
	"ByteDance":  "Doubao.Color",
	"Kuaishou":   "Kling.Color",
	"Jimeng":     "Jimeng.Color",
	"Vidu":       "Vidu",
	"Microsoft":  "AzureAI",
	"Azure":      "AzureAI",
}

// initDefaultVendorMapping 简化的默认供应商映射
func InitDefaultVendorMapping(metaMap map[string]*BuildModel, vendorMap map[int]*BuildVendor, enableAbilities []BuildAbility) {
	patterns := make([]string, 0, len(defaultVendorRules))
	for pattern := range defaultVendorRules {
		patterns = append(patterns, pattern)
	}
	slices.SortFunc(patterns, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	for _, ability := range enableAbilities {
		modelName := ability.Model
		if _, exists := metaMap[modelName]; exists {
			continue
		}

		vendorID := 0
		modelLower := strings.ToLower(modelName)
		for _, pattern := range patterns {
			vendorName := defaultVendorRules[pattern]
			if strings.Contains(modelLower, pattern) {
				vendorID = GetDisplayVendor(vendorName, vendorMap)
				break
			}
		}

		metaMap[modelName] = &BuildModel{
			ModelName: modelName,
			VendorID:  vendorID,
			Status:    1,
			NameRule:  0,
		}
	}
}

// Default vendor entries are presentation data. Reading pricing must never
// recreate a deleted/merged database record.
var defaultVendorDisplayIDs = map[string]int{
	"360":        -1001,
	"Anthropic":  -1002,
	"Cloudflare": -1003,
	"Cohere":     -1004,
	"DeepSeek":   -1005,
	"Google":     -1006,
	"Jina":       -1007,
	"Meta":       -1008,
	"MiniMax":    -1009,
	"Mistral":    -1010,
	"Moonshot":   -1011,
	"OpenAI":     -1012,
	"Vidu":       -1013,
	"xAI":        -1014,
	"Jimeng":     -1015,
	"ByteDance":  -1016,
	"Kuaishou":   -1017,
	"Zhipu AI":   -1018,
	"Baidu":      -1019,
	"Tencent":    -1020,
	"iFlytek":    -1021,
	"Alibaba":    -1022,
	"01.AI":      -1023,
}

func GetDisplayVendor(vendorName string, vendorMap map[int]*BuildVendor) int {
	for id, vendor := range vendorMap {
		if strings.EqualFold(vendor.Name, vendorName) {
			return id
		}
	}
	id := defaultVendorDisplayIDs[vendorName]
	if id == 0 {
		return 0
	}
	vendorMap[id] = &BuildVendor{Id: id, Name: vendorName, Status: 1, Icon: GetDefaultVendorIcon(vendorName)}
	return id
}

// 获取供应商默认图标
func GetDefaultVendorIcon(vendorName string) string {
	if icon, exists := defaultVendorIcons[vendorName]; exists {
		return icon
	}
	return ""
}
func PricingCatalogueFingerprint(pricing []Pricing, vendors []PricingVendor) (string, error) {
	rows := make([]string, 0, len(pricing))
	for _, item := range pricing {
		groups := append([]string{}, item.EnableGroup...)
		slices.Sort(groups)
		endpoints := append([]constant.EndpointType{}, item.SupportedEndpointTypes...)
		slices.Sort(endpoints)
		offers, err := PricingFingerprintOffers(item.CatalogOffersByGroup)
		if err != nil {
			return "", err
		}
		publicOffers, err := PricingFingerprintOffers(item.PublicCatalogOffersByGroup)
		if err != nil {
			return "", err
		}
		canonical := make(map[string]string, len(item.ConsumerCanonicalModelsByGroup))
		for group, base := range item.ConsumerCanonicalModelsByGroup {
			canonical[group] = base
		}
		upstreamRoutes := make(map[string][]string, len(item.UpstreamCatalogRoutesByGroup))
		for group, routes := range item.UpstreamCatalogRoutesByGroup {
			for _, route := range routes {
				policy := ""
				if route.ParamOverride != nil {
					policy = commonHash([]byte(*route.ParamOverride))
				}
				upstreamRoutes[group] = append(upstreamRoutes[group], fmt.Sprintf("semantic-v3:%d:%s:%s:%s:%s:%s:%s:%t", route.ChannelID, route.ModelID, route.DocumentHash, route.Operation, route.Mode, route.Endpoint, policy, route.ServiceTierEnabled))
			}
			slices.Sort(upstreamRoutes[group])
		}
		row := struct {
			ModelName, Description, Icon, Tags, OwnerBy, DisplayName, CatalogModality string
			CatalogType                                                               string
			UpstreamRoutes                                                            map[string][]string
			VendorID                                                                  int
			Groups                                                                    []string
			Endpoints                                                                 []constant.EndpointType
			Offers                                                                    map[string][]string
			CanonicalGroups                                                           map[string]string
			PublicOffers                                                              map[string][]string
		}{item.ModelName, item.Description, item.Icon, item.Tags, item.OwnerBy, item.DisplayName, item.CatalogModality, item.CatalogType, upstreamRoutes, item.VendorID, groups, endpoints, offers, canonical, publicOffers}
		value, err := common.Marshal(row)
		if err != nil {
			return "", err
		}
		rows = append(rows, string(value))
	}
	slices.Sort(rows)
	creators := append([]PricingVendor{}, vendors...)
	slices.SortFunc(creators, func(left, right PricingVendor) int { return left.ID - right.ID })
	value, err := common.Marshal(struct {
		Rows    []string
		Vendors []PricingVendor
	}{rows, creators})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:]), nil
}
func PricingFingerprintOffers(byGroup map[string][]CatalogOffer) (map[string][]string, error) {
	offers := make(map[string][]string, len(byGroup))
	for group, candidates := range byGroup {
		offers[group] = make([]string, 0, len(candidates))
		for _, offer := range candidates {
			offer.Prices = append([]CatalogPrice{}, offer.Prices...)
			slices.SortFunc(offer.Prices, comparePricingFingerprintPrices)
			value, err := common.Marshal(offer)
			if err != nil {
				return nil, err
			}
			offers[group] = append(offers[group], string(value))
		}
		slices.Sort(offers[group])
	}
	return offers, nil
}
func commonHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

type OperationRuntime struct {
	CatalogEntry                     func(*BuildChannel, string, constant.EndpointType) (PricingCatalogEntry, bool)
	AudioRatio, AudioCompletionRatio func(string) float64
	BatchExpression                  func(string) (string, bool)
}
type Operations struct{ runtime OperationRuntime }

func NewOperations(runtime OperationRuntime) *Operations { return &Operations{runtime: runtime} }

// ConfiguredVeoUsageOffers projects the declared Google duration-rate shape only.
// Unsupported expressions remain unknown; this never evaluates task billing.
func ConfiguredVeoUsageOffers(pricing Pricing, channel *BuildChannel, upstream string, generation PluginGeneration) []CatalogOffer {
	unknown := []CatalogOffer{
		{ID: "configured:video:unknown", Operation: constant.TaskActionTextToVideo, Mode: "sync", MainComponent: "video_output"},
		{ID: "configured:image-video:unknown", Operation: constant.TaskActionImageToVideo, Mode: "sync", MainComponent: "video_output"},
	}
	var schema map[string]jsplugin.UsageFieldSchema
	for _, candidate := range generation.LookupEndpointCandidates("POST", "/v1/videos", upstream) {
		if candidate.Plugin != nil && candidate.Plugin.Meta.Key == "google" && slices.Contains(candidate.Plugin.Meta.ChannelTypes, channel.Type) {
			schema = candidate.Plugin.Meta.UsageSchema
			break
		}
	}
	_, body := billingexpr.ParseExprVersion(pricing.BillingExpr)
	tree, err := parser.Parse(body)
	if err != nil || schema == nil || pricing.BillingMode != BillingModeTieredExpr {
		return unknown
	}
	root := tree.Node
	if call, ok := root.(*ast.CallNode); ok {
		name, named := call.Callee.(*ast.IdentifierNode)
		if !named || name.Value != "tier" || len(call.Arguments) != 2 {
			return unknown
		}
		if _, literal := call.Arguments[0].(*ast.StringNode); !literal {
			return unknown
		}
		root = call.Arguments[1]
	}
	product, ok := root.(*ast.BinaryNode)
	if !ok || product.Operator != "*" {
		return unknown
	}
	usage, ok := product.Left.(*ast.CallNode)
	if !ok || len(usage.Arguments) != 1 {
		return unknown
	}
	callee, ok := usage.Callee.(*ast.IdentifierNode)
	field, literal := usage.Arguments[0].(*ast.StringNode)
	if !ok || callee.Value != "u" || !literal || schema[field.Value].Type != "number" || schema[field.Value].Unit != "second" {
		return unknown
	}
	type branch struct {
		node   ast.Node
		guards []jsplugin.CatalogUsageCondition
		depth  int
	}
	pending := []branch{{node: product.Right}}
	var result []CatalogOffer
	for len(pending) > 0 {
		if len(pending)+len(result) > 64 {
			return unknown
		}
		current := pending[0]
		pending = pending[1:]
		if current.depth > 16 {
			return unknown
		}
		if conditional, ok := current.node.(*ast.ConditionalNode); ok {
			comparison, ok := conditional.Cond.(*ast.BinaryNode)
			if !ok || comparison.Operator != "==" {
				return unknown
			}
			probe, ok := comparison.Left.(*ast.CallNode)
			value, literal := comparison.Right.(*ast.StringNode)
			if !ok || !literal || len(probe.Arguments) != 1 {
				return unknown
			}
			name, ok := probe.Callee.(*ast.IdentifierNode)
			key, literal := probe.Arguments[0].(*ast.StringNode)
			if !ok || name.Value != "u" || !literal || !slices.Contains(schema[key.Value].Enum, value.Value) {
				return unknown
			}
			guard := jsplugin.CatalogUsageCondition{Field: key.Value, Value: value.Value}
			then := append(slices.Clone(current.guards), guard)
			guard.Negated = true
			otherwise := append(slices.Clone(current.guards), guard)
			pending = append(pending, branch{conditional.Exp1, then, current.depth + 1}, branch{conditional.Exp2, otherwise, current.depth + 1})
			continue
		}
		for key, field := range schema {
			if len(field.Enum) == 0 {
				continue
			}
			possible := false
			for _, value := range field.Enum {
				matches := true
				for _, guard := range current.guards {
					if guard.Field == key && ((value == guard.Value) == guard.Negated) {
						matches = false
						break
					}
				}
				possible = possible || matches
			}
			if !possible {
				return unknown
			}
		}
		var amount float64
		switch number := current.node.(type) {
		case *ast.IntegerNode:
			amount = float64(number.Value)
		case *ast.FloatNode:
			amount = number.Value
		default:
			return unknown
		}
		if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return unknown
		}
		for _, action := range []string{constant.TaskActionTextToVideo, constant.TaskActionImageToVideo} {
			result = append(result, CatalogOffer{ID: fmt.Sprintf("configured:%s:%d", action, len(result)), Operation: action, Mode: "sync", MainComponent: "video_output", PublicUsageConditions: current.guards, PublicConditional: len(current.guards) > 0, Prices: []CatalogPrice{{Component: "video_output", Unit: "second", AmountUSD: &amount}}})
		}
	}
	return result
}
func comparePricingFingerprintPrices(left, right CatalogPrice) int {
	if left.Component < right.Component {
		return -1
	}
	if left.Component > right.Component {
		return 1
	}
	if left.Unit < right.Unit {
		return -1
	}
	if left.Unit > right.Unit {
		return 1
	}
	return 0
}
