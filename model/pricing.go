package model

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

const (
	BillingSourceConfiguredTariff                           = "configured_tariff"
	BillingSourceOpenRouterActualCost                       = "openrouter_actual_cost"
	PricingEndpointBatch              constant.EndpointType = "openai_batch"
)

type Pricing struct {
	CatalogType                     string                                        `json:"-"`
	CatalogModality                 string                                        `json:"-"`
	DisplayName                     string                                        `json:"display_name,omitempty"`
	UpstreamCatalogRoutesByGroup    map[string][]UpstreamCatalogRoute             `json:"-"`
	PublicCatalogOffersByGroup      map[string][]CatalogOffer                     `json:"-"`
	ConsumerCanonicalModelsByGroup  map[string]string                             `json:"-"`
	ConfiguredPrices                map[string][][]billingexpr.DisplayPrice       `json:"-"`
	PriceConfigured                 bool                                          `json:"-"`
	CatalogOffersByGroup            map[string][]CatalogOffer                     `json:"-"`
	BillingSourcesByGroup           map[string]map[constant.EndpointType][]string `json:"billing_sources_by_group,omitempty"`
	IndicativeBillingExprByEndpoint map[constant.EndpointType]string              `json:"indicative_billing_expr_by_endpoint,omitempty"`
	ModelName                       string                                        `json:"model_name"`
	Description                     string                                        `json:"description,omitempty"`
	Icon                            string                                        `json:"icon,omitempty"`
	Tags                            string                                        `json:"tags,omitempty"`
	VendorID                        int                                           `json:"vendor_id,omitempty"`
	QuotaType                       int                                           `json:"quota_type"`
	ModelRatio                      float64                                       `json:"model_ratio"`
	ModelPrice                      float64                                       `json:"model_price"`
	OwnerBy                         string                                        `json:"owner_by"`
	CompletionRatio                 float64                                       `json:"completion_ratio"`
	CacheRatio                      *float64                                      `json:"cache_ratio,omitempty"`
	CreateCacheRatio                *float64                                      `json:"create_cache_ratio,omitempty"`
	ImageRatio                      *float64                                      `json:"image_ratio,omitempty"`
	AudioRatio                      *float64                                      `json:"audio_ratio,omitempty"`
	AudioCompletionRatio            *float64                                      `json:"audio_completion_ratio,omitempty"`
	EnableGroup                     []string                                      `json:"enable_groups"`
	SupportedEndpointTypes          []constant.EndpointType                       `json:"supported_endpoint_types"`
	BillingMode                     string                                        `json:"billing_mode,omitempty"`
	BillingExpr                     string                                        `json:"billing_expr,omitempty"`
	BillingUsageSchema              map[string]jsplugin.UsageFieldSchema          `json:"billing_usage_schema,omitempty"`
	BillingUsageExamples            []jsplugin.UsageExample                       `json:"billing_usage_examples,omitempty"`
	PricingVersion                  string                                        `json:"pricing_version,omitempty"`

	LegacyVideoPriceBasisByGroup map[string][]map[string]jsplugin.LegacyPriceBasis `json:"-"`
}

func (p Pricing) LegacyVideoPriceBasisForGroup(group string) (map[string]jsplugin.LegacyPriceBasis, bool) {
	var basis map[string]jsplugin.LegacyPriceBasis
	for _, sourceGroup := range []string{"all", group} {
		for _, routeBasis := range p.LegacyVideoPriceBasisByGroup[sourceGroup] {
			if len(routeBasis) == 0 || (basis != nil && !maps.Equal(basis, routeBasis)) {
				return nil, false
			}
			basis = routeBasis
		}
	}
	return maps.Clone(basis), len(basis) > 0
}

func legacyVideoPriceBasis(generation *jsplugin.RoutingGeneration, channel *Channel, modelName, taskPluginKey string) (map[string]jsplugin.LegacyPriceBasis, bool, bool) {
	if channel == nil {
		return nil, false, false
	}
	declared, known := generation.CanonicalModel(modelName)
	if !known {
		mapping := make(map[string]string)
		if err := common.UnmarshalJsonStr(channel.GetModelMapping(), &mapping); err != nil {
			return nil, false, false
		}
		tail, cyclic := followChannelModelMapping(mapping, modelName)
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

// BillingSourcesForGroup expands universal abilities without modifying the cached snapshot.
func (p Pricing) BillingSourcesForGroup(group string) map[constant.EndpointType][]string {
	sourcesByEndpoint := make(map[constant.EndpointType][]string)
	for _, sourceGroup := range []string{"all", group} {
		for endpoint, sources := range p.BillingSourcesByGroup[sourceGroup] {
			for _, source := range sources {
				if !slices.Contains(sourcesByEndpoint[endpoint], source) {
					sourcesByEndpoint[endpoint] = append(sourcesByEndpoint[endpoint], source)
				}
			}
		}
	}
	return sourcesByEndpoint
}

type PricingVendor struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

var (
	pricingMap           []Pricing
	vendorsList          []PricingVendor
	supportedEndpointMap map[string]common.EndpointInfo
	lastGetPricingTime   time.Time
	updatePricingLock    sync.Mutex
	pricingRevision      atomic.Uint64

	// 缓存映射：模型名 -> 启用分组 / 计费类型
	modelEnableGroups     = make(map[string][]string)
	modelQuotaTypeMap     = make(map[string]int)
	modelEnableGroupsLock = sync.RWMutex{}
)

var (
	modelSupportEndpointTypes = make(map[string][]constant.EndpointType)
	modelSupportEndpointsLock = sync.RWMutex{}
)

func GetPricingCatalogue() ([]Pricing, []PricingVendor) {
	pricing, vendors, _, _ := GetPricingCatalogueSnapshot()
	return pricing, vendors
}

func refreshPricingCatalogueLocked() {
	if (pricingBuiltGeneration != pricingInputsGeneration.Load() || lastGetPricingTime.IsZero() || pricingBuiltPluginGeneration != jsplugin.DefaultRegistry.Generation().Number) && pricingBuildDue() {
		modelSupportEndpointsLock.Lock()
		defer modelSupportEndpointsLock.Unlock()
		rebuildPricingCatalogueLocked()
	}
}

func GetPricing() []Pricing {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()
	refreshPricingCatalogueLocked()
	return pricingMap
}

// GetPricingRevision identifies the currently published in-memory pricing
// snapshot. Callers should read it after GetPricing so an expired snapshot has
// already been refreshed.
func GetPricingRevision() uint64 {
	return pricingRevision.Load()
}

func InvalidatePricingCache() {
	pricingPublicationLock.Lock()
	defer pricingPublicationLock.Unlock()
	pricingInputsGeneration.Add(1)
}

func GetVendors() []PricingVendor {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()
	refreshPricingCatalogueLocked()
	return vendorsList
}

func GetModelSupportEndpointTypes(model string) []constant.EndpointType {
	if model == "" {
		return make([]constant.EndpointType, 0)
	}
	modelSupportEndpointsLock.RLock()
	defer modelSupportEndpointsLock.RUnlock()
	if endpoints, ok := modelSupportEndpointTypes[model]; ok {
		return endpoints
	}
	return make([]constant.EndpointType, 0)
}

func getPricingEndpointTypesForAbility(ability AbilityWithChannel, advancedCustomConfigs map[int]*dto.AdvancedCustomConfig) []constant.EndpointType {
	if ability.ChannelType != constant.ChannelTypeAdvancedCustom {
		endpoints := common.GetEndpointTypesByChannelType(ability.ChannelType, ability.Model)
		return endpoints
	}
	if config := advancedCustomConfigs[ability.ChannelId]; config != nil {
		return config.SupportedEndpointTypesForModel(ability.Model)
	}
	return common.GetEndpointTypesByChannelType(ability.ChannelType, ability.Model)
}

func appendPricingEndpoint(endpoints []string, endpoint string) []string {
	if endpoint == "" || common.StringsContains(endpoints, endpoint) {
		return endpoints
	}
	return append(endpoints, endpoint)
}

func rebuildPricingCatalogueLocked() {
	generation := pricingInputsGeneration.Load()
	pluginGeneration := jsplugin.DefaultRegistry.Generation()
	pricingAttemptGeneration.Store(generation)
	pricingAttemptPluginGeneration.Store(pluginGeneration.Number)
	published := false
	defer func() {
		if !published {
			pricingRetryAfter.Store(time.Now().Add(pricingBuildRetryDelay).UnixNano())
		}
	}()
	//modelRatios := common.GetModelRatios()
	enableAbilities, err := GetAllEnableAbilityWithChannels()
	if err != nil {
		common.SysLog(fmt.Sprintf("GetAllEnableAbilityWithChannels error: %v", err))
		return
	}
	// 预加载模型元数据与供应商一次，避免循环查询
	var allMeta []Model
	if err := DB.Find(&allMeta).Error; err != nil {
		return
	}
	names := make([]string, 0, len(enableAbilities))
	for _, ability := range enableAbilities {
		names = append(names, ability.Model)
	}
	metaMap := resolveModelMetadata(allMeta, names)

	// 预加载供应商
	var vendors []Vendor
	if err := DB.Find(&vendors).Error; err != nil {
		return
	}
	var channels []*Channel
	if err := DB.Find(&channels).Error; err != nil {
		return
	}
	sourceFingerprint, err := pricingPersistedInputFingerprint(allMeta, vendors, channels, enableAbilities)
	if err != nil {
		return
	}
	channelFingerprint, err := pricingPersistedInputFingerprint(nil, nil, channels, enableAbilities)
	if err != nil {
		return
	}
	vendorMap := make(map[int]*Vendor)
	for i := range vendors {
		vendorMap[vendors[i].Id] = &vendors[i]
	}

	// Stored model rows own their vendor; name heuristics are only a fallback below catalog authors.
	storedMeta := make(map[string]bool, len(metaMap))
	for name := range metaMap {
		storedMeta[name] = true
	}
	// 初始化默认供应商映射
	initDefaultVendorMapping(metaMap, vendorMap, enableAbilities)

	modelGroupsMap := make(map[string]*types.Set[string])

	for _, ability := range enableAbilities {
		groups, ok := modelGroupsMap[ability.Model]
		if !ok {
			groups = types.NewSet[string]()
			modelGroupsMap[ability.Model] = groups
		}
		groups.Add(ability.Group)
	}

	//这里使用切片而不是Set，因为一个模型可能支持多个端点类型，并且第一个端点是优先使用端点
	modelSupportEndpointsStr := make(map[string][]string)
	pricingChannels := make(map[int]*Channel, len(channels))
	advancedCustomConfigs := make(map[int]*dto.AdvancedCustomConfig)
	taskPluginKeys := make(map[int]string)
	serviceTierEnabled := make(map[int]bool)
	referenced := make(map[int]bool)
	for _, ability := range enableAbilities {
		referenced[ability.ChannelId] = true
	}
	for _, channel := range channels {
		if !referenced[channel.Id] {
			continue
		}
		pricingChannels[channel.Id] = channel
		if channel.Status != common.ChannelStatusEnabled {
			continue
		}
		// Pricing owns a read-only source snapshot. Channel getters may repair and
		// save malformed rows, so decode only relevant inputs without those getters.
		var settings dto.ChannelSettings
		if channel.Setting != nil && *channel.Setting != "" {
			if err := common.UnmarshalJsonStr(*channel.Setting, &settings); err != nil {
				common.SysLog(fmt.Sprintf("pricing channel setting invalid: channel_id=%d", channel.Id))
				return
			}
		}
		var other dto.ChannelOtherSettings
		if channel.OtherSettings != "" {
			if err := common.UnmarshalJsonStr(channel.OtherSettings, &other); err != nil {
				common.SysLog(fmt.Sprintf("pricing channel settings invalid: channel_id=%d", channel.Id))
				return
			}
		}
		if channel.ModelMapping != nil && strings.TrimSpace(*channel.ModelMapping) != "" {
			var mapping map[string]string
			if err := common.UnmarshalJsonStr(*channel.ModelMapping, &mapping); err != nil {
				common.SysLog(fmt.Sprintf("pricing channel mapping invalid: channel_id=%d", channel.Id))
				return
			}
		}
		serviceTierEnabled[channel.Id] = other.AllowServiceTier || settings.PassThroughBodyEnabled || model_setting.GetGlobalSettings().PassThroughRequestEnabled
		taskPluginKeys[channel.Id] = settings.TaskPluginKey
		if channel.Type == constant.ChannelTypeAdvancedCustom && other.AdvancedCustom != nil {
			advancedCustomConfigs[channel.Id] = other.AdvancedCustom
		}
	}
	billingSources := make(map[string]map[string]map[constant.EndpointType][]string)
	legacyVideoBases := make(map[string]map[string][]map[string]jsplugin.LegacyPriceBasis)
	for _, ability := range enableAbilities {
		if pricingChannels[ability.ChannelId] == nil {
			return
		}
	}

	// 先根据已有能力填充原生端点
	for _, ability := range enableAbilities {
		endpoints := modelSupportEndpointsStr[ability.Model]
		channelTypes := getPricingEndpointTypesForAbility(ability, advancedCustomConfigs)
		if billingSources[ability.Model] == nil {
			billingSources[ability.Model] = make(map[string]map[constant.EndpointType][]string)
		}
		if billingSources[ability.Model][ability.Group] == nil {
			billingSources[ability.Model][ability.Group] = make(map[constant.EndpointType][]string)
		}
		source := BillingSourceConfiguredTariff
		openRouterVideo := ability.ChannelType == constant.ChannelTypeTaskPlugin && taskPluginKeys[ability.ChannelId] == "openrouter-video"
		if ability.ChannelType == constant.ChannelTypeOpenRouter || openRouterVideo {
			source = BillingSourceOpenRouterActualCost
		}
		legacyBasis, videoRoute, videoOnly := legacyVideoPriceBasis(pluginGeneration, pricingChannels[ability.ChannelId], ability.Model, taskPluginKeys[ability.ChannelId])
		if videoOnly && ability.ChannelType != constant.ChannelTypeAdvancedCustom {
			channelTypes = slices.DeleteFunc(slices.Clone(channelTypes), func(endpoint constant.EndpointType) bool {
				return endpoint == constant.EndpointTypeImageGeneration
			})
		}
		if videoRoute && !slices.Contains(channelTypes, constant.EndpointTypeOpenAIVideo) {
			channelTypes = append(channelTypes, constant.EndpointTypeOpenAIVideo)
		}
		// Batch is an operation with a separate display expression, not a sync discount.
		operationTypes := slices.Clone(channelTypes)
		if ability.ChannelType == constant.ChannelTypeOpenRouter {
			operationTypes = append(operationTypes, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact, constant.EndpointTypeEmbeddings, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini)
		}
		if videoRoute || openRouterVideo {
			operationTypes = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
		}
		if !videoRoute && slices.Contains([]int{constant.ChannelTypeOpenAI, constant.ChannelTypeOpenRouter, constant.ChannelTypeAnthropic, constant.ChannelTypeGemini}, ability.ChannelType) {
			operationTypes = append(operationTypes, PricingEndpointBatch)
		}
		for _, endpoint := range operationTypes {
			if endpoint == constant.EndpointTypeOpenAIVideo && source == BillingSourceConfiguredTariff {
				if legacyVideoBases[ability.Model] == nil {
					legacyVideoBases[ability.Model] = make(map[string][]map[string]jsplugin.LegacyPriceBasis)
				}
				legacyVideoBases[ability.Model][ability.Group] = append(legacyVideoBases[ability.Model][ability.Group], legacyBasis)
			}
			sources := billingSources[ability.Model][ability.Group][endpoint]
			if !slices.Contains(sources, source) {
				billingSources[ability.Model][ability.Group][endpoint] = append(sources, source)
			}
		}
		for _, channelType := range channelTypes {
			if !common.StringsContains(endpoints, string(channelType)) {
				endpoints = append(endpoints, string(channelType))
			}
		}
		modelSupportEndpointsStr[ability.Model] = endpoints
	}

	// 再补充模型自定义端点：若配置有效则追加到已有推断，不再裁剪渠道真实能力
	for modelName, meta := range metaMap {
		if strings.TrimSpace(meta.Endpoints) == "" {
			continue
		}
		var raw map[string]any
		if err := common.Unmarshal([]byte(meta.Endpoints), &raw); err == nil {
			endpoints := modelSupportEndpointsStr[modelName]
			for k, v := range raw {
				switch v.(type) {
				case string, map[string]any:
					endpoints = appendPricingEndpoint(endpoints, k)
				}
			}
			if len(endpoints) > 0 {
				modelSupportEndpointsStr[modelName] = endpoints
			}
		}
	}

	nextEndpointTypes := make(map[string][]constant.EndpointType)
	for model, endpoints := range modelSupportEndpointsStr {
		supportedEndpoints := make([]constant.EndpointType, 0)
		for _, endpointStr := range endpoints {
			endpointType := constant.EndpointType(endpointStr)
			supportedEndpoints = append(supportedEndpoints, endpointType)
		}
		nextEndpointTypes[model] = supportedEndpoints
	}

	// 构建全局 nextSupportedEndpoints（默认 + 自定义覆盖）
	nextSupportedEndpoints := make(map[string]common.EndpointInfo)
	// 1. 默认端点
	for _, endpoints := range nextEndpointTypes {
		for _, et := range endpoints {
			if info, ok := common.GetDefaultEndpointInfo(et); ok {
				if _, exists := nextSupportedEndpoints[string(et)]; !exists {
					nextSupportedEndpoints[string(et)] = info
				}
			}
		}
	}
	// 2. 自定义端点（models 表）覆盖默认
	for _, meta := range metaMap {
		if strings.TrimSpace(meta.Endpoints) == "" {
			continue
		}
		var raw map[string]any
		if err := common.Unmarshal([]byte(meta.Endpoints), &raw); err == nil {
			for k, v := range raw {
				switch val := v.(type) {
				case string:
					nextSupportedEndpoints[k] = common.EndpointInfo{Path: val, Method: "POST"}
				case map[string]any:
					ep := common.EndpointInfo{Method: "POST"}
					if p, ok := val["path"].(string); ok {
						ep.Path = p
					}
					if m, ok := val["method"].(string); ok {
						ep.Method = strings.ToUpper(m)
					}
					nextSupportedEndpoints[k] = ep
				default:
					// ignore unsupported types
				}
			}
		}
	}

	upstreamDocuments, upstreamHashes, upstreamEnabled, err := loadConfiguredCatalog(context.Background())
	if err != nil {
		return
	}
	upstreamRoutes := make(map[string]map[string][]UpstreamCatalogRoute)
	upstreamMetadata := make(map[string]*catalogsync.Document)
	upstreamTypes := make(map[string]string)
	catalogOffers := make(map[string]map[string][]CatalogOffer)
	for _, ability := range enableAbilities {
		channel := pricingChannels[ability.ChannelId]
		if channel == nil || channel.Status != 1 {
			continue
		}
		if upstreamEnabled && IsOpenRouterCatalogChannel(channel) {
			upstream, resolved := ResolvePricingUpstreamModel(channel, ability.Model)
			if !resolved {
				continue
			}
			original := upstreamDocuments[upstream]
			if original == nil {
				continue
			}
			canonical, resolveErr := catalogsync.ResolveAlias(upstreamDocuments, upstream)
			if resolveErr != nil {
				return
			}
			if prior := upstreamMetadata[ability.Model]; prior == nil || catalogsync.CompareIDs(original.ID, prior.ID) < 0 {
				upstreamMetadata[ability.Model] = original
				if slices.Contains(catalogsync.ModelTypes(), canonical.Type) {
					upstreamTypes[ability.Model] = canonical.Type
				} else {
					delete(upstreamTypes, ability.Model)
				}
			}
			consumerEndpoints := make([]constant.EndpointType, 0, len(billingSources[ability.Model][ability.Group]))
			for endpoint, sources := range billingSources[ability.Model][ability.Group] {
				if !slices.Contains(sources, BillingSourceOpenRouterActualCost) {
					continue
				}
				consumerEndpoints = append(consumerEndpoints, endpoint)
			}
			consumerDocument := original
			if canonical.Type == "decision" {
				// Type20 has a native SystemOne transport, not a Chat decision
				// endpoint. Require a published eligible native endpoint as well
				// as declared decisions output before projecting its rates.
				consumerDocument = canonical
				consumerEndpoints = nil
				if slices.ContainsFunc(canonical.Offers, func(offer catalogsync.Offer) bool {
					mode, known := CatalogOfferMode(offer, upstream)
					return offer.Scope == "endpoint" && offer.Eligibility != nil && offer.Eligibility.Eligible &&
						(offer.Operation == nil || *offer.Operation == "decisions") && known && mode == "sync"
				}) {
					consumerEndpoints = []constant.EndpointType{PricingEndpointSystemOne}
				}
			}
			for _, endpoint := range consumerEndpoints {
				operation := catalogConsumerOperation(consumerDocument, upstream, endpoint)
				if operation.operation == "" {
					continue
				}
				mode := "sync"
				if endpoint == PricingEndpointBatch {
					mode = "batch"
				}
				if upstreamRoutes[ability.Model] == nil {
					upstreamRoutes[ability.Model] = make(map[string][]UpstreamCatalogRoute)
				}
				upstreamRoutes[ability.Model][ability.Group] = append(upstreamRoutes[ability.Model][ability.Group], UpstreamCatalogRoute{Operation: operation.operation, Mode: mode, Endpoint: endpoint, ModelID: upstream, DocumentHash: upstreamHashes[canonical.ID], Document: canonical, ChannelID: channel.Id, ParamOverride: channel.ParamOverride, ServiceTierEnabled: serviceTierEnabled[channel.Id]})
			}
			continue
		}
		if channel.Type == constant.ChannelTypeOpenRouter {
			baseURL := channel.GetBaseURL()
			if baseURL == "" {
				baseURL = constant.GetChannelBaseURL(channel.Type)
			}
			if strings.TrimRight(baseURL, "/") != "https://openrouter.ai/api" {
				continue
			}
			if channel.ParamOverride != nil && strings.TrimSpace(*channel.ParamOverride) != "" && strings.TrimSpace(*channel.ParamOverride) != "{}" {
				continue
			}
		}
		upstream, resolved := ResolvePricingUpstreamModel(channel, ability.Model)
		if !resolved {
			continue
		}
		plugin, known := pluginGeneration.GetByChannelType(channel.Type)
		if channel.Type == constant.ChannelTypeTaskPlugin {
			plugin, known = pluginGeneration.Get(taskPluginKeys[channel.Id])
		}
		var offers []CatalogOffer
		if known && plugin.Meta.PriceCatalog != nil {
			if declared, declaredModel := pluginGeneration.CanonicalModel(upstream); declaredModel {
				for _, candidate := range pluginGeneration.LookupEndpointCandidates("POST", "/v1/videos", declared) {
					if candidate.Plugin == plugin {
						offers = plugin.Meta.PriceCatalog.Models[declared]
						break
					}
				}
			}
		}
		if offers == nil && channel.Type == constant.ChannelTypeOpenRouter {
			if source, known := GetPricingCatalogEntry(channel, upstream, constant.EndpointTypeImageGeneration); known {
				offers = append(offers, source.Offers...)
			}
			for endpoint, sources := range billingSources[ability.Model][ability.Group] {
				if endpoint == constant.EndpointTypeImageGeneration {
					continue
				}
				if !slices.Contains(sources, BillingSourceOpenRouterActualCost) {
					continue
				}
				if source, known := GetPricingCatalogEntry(channel, upstream, endpoint); known {
					offers = append(offers, source.Offers...)
				}
			}
		}
		if len(offers) == 0 {
			continue
		}
		if catalogOffers[ability.Model] == nil {
			catalogOffers[ability.Model] = make(map[string][]CatalogOffer)
		}
		for _, offer := range offers {
			prefix := fmt.Sprintf("%d:%s:", ability.ChannelId, upstream)
			offer.ID = prefix + offer.ID
			offer.PublicAlternatives = slices.Clone(offer.PublicAlternatives)
			for i := range offer.PublicAlternatives {
				offer.PublicAlternatives[i].ID = prefix + offer.PublicAlternatives[i].ID
			}
			catalogOffers[ability.Model][ability.Group] = append(catalogOffers[ability.Model][ability.Group], offer)
		}
	}
	nextPricing := make([]Pricing, 0)
	projectedExpressions := make(map[string][][]billingexpr.DisplayPrice)
	for model, groups := range modelGroupsMap {
		pricing := Pricing{
			UpstreamCatalogRoutesByGroup: upstreamRoutes[model],
			CatalogOffersByGroup:         catalogOffers[model],
			LegacyVideoPriceBasisByGroup: legacyVideoBases[model],
			ModelName:                    model,
			BillingSourcesByGroup:        billingSources[model],
			EnableGroup:                  groups.Items(),
			SupportedEndpointTypes:       nextEndpointTypes[model],
		}

		// 补充模型元数据（描述、标签、供应商、状态）
		if meta, ok := metaMap[model]; ok {
			// 若模型被禁用(status!=1)，则直接跳过，不返回给前端
			if meta.Status != 1 {
				continue
			}
			pricing.Description = meta.Description
			pricing.Icon = meta.Icon
			pricing.Tags = meta.Tags
			pricing.VendorID = meta.VendorID
		}
		if upstream := upstreamMetadata[model]; upstream != nil {
			_, pricing.CatalogModality = catalogConsumerProfile(upstream)
			pricing.CatalogType = upstreamTypes[model]
			pricing.DisplayName = upstream.Name
			if pricing.Description == "" && upstream.Description != nil {
				pricing.Description = *upstream.Description
			}
			if !storedMeta[model] || pricing.VendorID == 0 {
				if vendorID := catalogAuthorVendor(upstream.Author, vendorMap); vendorID != 0 {
					pricing.VendorID = vendorID
				}
			}
		}
		modelPrice, findPrice := ratio_setting.GetModelPrice(model, false)
		pricing.PriceConfigured = findPrice || ratio_setting.HasConfiguredModelRatio(model)
		if findPrice {
			pricing.ModelPrice = modelPrice
			pricing.QuotaType = 1
		} else {
			modelRatio, _, _ := ratio_setting.GetModelRatio(model)
			pricing.ModelRatio = modelRatio
			pricing.CompletionRatio = ratio_setting.GetCompletionRatio(model)
			pricing.QuotaType = 0
		}
		if cacheRatio, ok := ratio_setting.GetCacheRatio(model); ok {
			pricing.CacheRatio = &cacheRatio
		}
		if createCacheRatio, ok := ratio_setting.GetCreateCacheRatio(model); ok {
			pricing.CreateCacheRatio = &createCacheRatio
		}
		if imageRatio, ok := ratio_setting.GetImageRatio(model); ok {
			pricing.ImageRatio = &imageRatio
		}
		if ratio_setting.ContainsAudioRatio(model) {
			audioRatio := ratio_setting.GetAudioRatio(model)
			pricing.AudioRatio = &audioRatio
		}
		if ratio_setting.ContainsAudioCompletionRatio(model) {
			audioCompletionRatio := ratio_setting.GetAudioCompletionRatio(model)
			pricing.AudioCompletionRatio = &audioCompletionRatio
		}
		if billingMode := billing_setting.GetBillingMode(model); billingMode == "tiered_expr" {
			if expr, ok := billing_setting.GetBillingExpr(model); ok && strings.TrimSpace(expr) != "" {
				pricing.BillingMode = billingMode
				pricing.BillingExpr = expr
			}
		} else if target, resolved := ResolveTaskModelAlias(pluginGeneration, model); resolved && target.Declared != "" {
			if tailMode := billing_setting.GetBillingMode(target.Declared); tailMode == "tiered_expr" {
				if expr, ok := billing_setting.GetBillingExpr(target.Declared); ok && strings.TrimSpace(expr) != "" {
					pricing.BillingMode = tailMode
					pricing.BillingExpr = expr
				}
			}
		}
		plugin, ok := pluginGeneration.GetByModel(model)
		if !ok {
			if target, resolved := ResolveTaskModelAlias(pluginGeneration, model); resolved {
				plugin, ok = pluginGeneration.Get(target.PluginKey)
			}
		}
		if ok && plugin != nil && len(plugin.Meta.UsageSchema) > 0 {
			pricing.BillingUsageSchema = make(map[string]jsplugin.UsageFieldSchema, len(plugin.Meta.UsageSchema))
			for key, field := range plugin.Meta.UsageSchema {
				field.Enum = append([]string(nil), field.Enum...)
				field.Description = maps.Clone(field.Description)
				if field.EnumLabels != nil {
					labels := make(map[string]jsplugin.LocalizedText, len(field.EnumLabels))
					for value, label := range field.EnumLabels {
						labels[value] = maps.Clone(label)
					}
					field.EnumLabels = labels
				}
				pricing.BillingUsageSchema[key] = field
			}
			if len(plugin.Meta.UsageExamples) > 0 {
				pricing.BillingUsageExamples = make([]jsplugin.UsageExample, len(plugin.Meta.UsageExamples))
				for index, example := range plugin.Meta.UsageExamples {
					facts := make(map[string]any, len(example.Facts))
					maps.Copy(facts, example.Facts)
					pricing.BillingUsageExamples[index] = jsplugin.UsageExample{
						Label: example.Label,
						Facts: facts,
					}
				}
			}
		}
		pricing.ConfiguredPrices = make(map[string][][]billingexpr.DisplayPrice)
		for _, endpoints := range pricing.BillingSourcesByGroup {
			for endpoint, sources := range endpoints {
				if !slices.Contains(sources, BillingSourceConfiguredTariff) {
					continue
				}
				expression := pricing.BillingExpr
				if endpoint == PricingEndpointBatch {
					expression, _ = billing_setting.GetBatchBillingExpr(model)
				}
				if expression == "" {
					continue
				}
				prices, exists := projectedExpressions[expression]
				if !exists {
					prices = billingexpr.CatalogPrices(expression)
					projectedExpressions[expression] = prices
				}
				pricing.ConfiguredPrices[expression] = prices
			}
		}
		pricing.IndicativeBillingExprByEndpoint = make(map[constant.EndpointType]string)
		syncExpression, _ := billing_setting.GetBillingExpr(model)
		if len(pricing.BillingUsageSchema) > 0 {
			syncExpression = pricing.BillingExpr
		}
		for _, endpoints := range pricing.BillingSourcesByGroup {
			for endpoint, sources := range endpoints {
				if !slices.Contains(sources, BillingSourceOpenRouterActualCost) {
					continue
				}
				expression := syncExpression
				if endpoint == PricingEndpointBatch {
					expression, _ = billing_setting.GetBatchBillingExpr(model)
				}
				pricing.IndicativeBillingExprByEndpoint[endpoint] = expression
			}
		}
		projectPricingOperations(&pricing, enableAbilities, pricingChannels, pluginGeneration, advancedCustomConfigs, taskPluginKeys)
		nextPricing = append(nextPricing, pricing)
	}

	// 防止大更新后数据不通用
	if len(nextPricing) > 0 {
		nextPricing[0].PricingVersion = "5a90f2b86c08bd983a9a2e6d66c255f4eaef9c4bc934386d2b6ae84ef0ff1f1f"
	}

	// 构建对前端友好的供应商列表
	nextVendors := make([]PricingVendor, 0, len(vendorMap))
	for _, v := range vendorMap {
		nextVendors = append(nextVendors, PricingVendor{
			ID:          v.Id,
			Name:        v.Name,
			Description: v.Description,
			Icon:        v.Icon,
		})
	}

	revision, err := pricingCatalogueFingerprint(nextPricing, nextVendors)
	if err != nil {
		return
	}
	pricingPublicationLock.Lock()
	defer pricingPublicationLock.Unlock()
	if generation != pricingInputsGeneration.Load() || pluginGeneration.Number != jsplugin.DefaultRegistry.Generation().Number {
		return
	}
	pricingMap, vendorsList = nextPricing, nextVendors
	modelSupportEndpointTypes, supportedEndpointMap = nextEndpointTypes, nextSupportedEndpoints
	pricingCatalogueRevision, pricingCatalogueRevisionError = revision, nil
	// 刷新缓存映射，供高并发快速查询
	modelEnableGroupsLock.Lock()
	modelEnableGroups = make(map[string][]string)
	modelQuotaTypeMap = make(map[string]int)
	for _, p := range nextPricing {
		modelEnableGroups[p.ModelName] = p.EnableGroup
		modelQuotaTypeMap[p.ModelName] = p.QuotaType
	}
	modelEnableGroupsLock.Unlock()
	lastGetPricingTime = time.Now()
	pricingRevision.Add(1)
	pricingBuiltGeneration = generation
	pricingBuiltPluginGeneration = pluginGeneration.Number
	published = true
	pricingRetryAfter.Store(0)
	pricingObservedSource.Store(&sourceFingerprint)
	pricingObservedChannels.Store(&channelFingerprint)
	publishedPricing.Store(&publishedPricingCatalogue{pricing: nextPricing, vendors: nextVendors, revision: revision, endpoints: nextSupportedEndpoints, builtAt: lastGetPricingTime, generation: generation, pluginGeneration: pluginGeneration.Number})
}

// GetSupportedEndpointMap 返回全局端点到路径的映射
func GetSupportedEndpointMap() map[string]common.EndpointInfo {
	snapshot := publishedPricing.Load()
	if snapshot == nil {
		return nil
	}
	return snapshot.endpoints
}
