package model

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

const (
	BillingSourceConfiguredTariff                           = "configured_tariff"
	BillingSourceOpenRouterActualCost                       = "openrouter_actual_cost"
	PricingEndpointBatch              constant.EndpointType = "openai_batch"
)

type Pricing struct {
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

func legacyVideoPriceBasis(generation *jsplugin.RoutingGeneration, channel *Channel, modelName string) (map[string]jsplugin.LegacyPriceBasis, bool, bool) {
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
			if channel.GetSetting().TaskPluginKey != meta.Key {
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
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()
	if time.Since(lastGetPricingTime) > time.Minute || len(pricingMap) == 0 {
		modelSupportEndpointsLock.Lock()
		defer modelSupportEndpointsLock.Unlock()
		updatePricing()
	}
	return pricingMap, vendorsList
}

func GetPricing() []Pricing {
	if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
		updatePricingLock.Lock()
		defer updatePricingLock.Unlock()
		// Double check after acquiring the lock
		if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
			modelSupportEndpointsLock.Lock()
			defer modelSupportEndpointsLock.Unlock()
			updatePricing()
		}
	}
	return pricingMap
}

// GetPricingRevision identifies the currently published in-memory pricing
// snapshot. Callers should read it after GetPricing so an expired snapshot has
// already been refreshed.
func GetPricingRevision() uint64 {
	return pricingRevision.Load()
}

func InvalidatePricingCache() {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()

	pricingMap = nil
	vendorsList = nil
	lastGetPricingTime = time.Time{}
}

// GetVendors 返回当前定价接口使用到的供应商信息
func GetVendors() []PricingVendor {
	if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
		// 保证先刷新一次
		GetPricing()
	}
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

// loadPricingAdvancedCustomConfigs runs inside updatePricing while
// updatePricingLock is held, and nests channelSyncLock.RLock. This defines the
// global lock order updatePricingLock -> channelSyncLock: any code path holding
// channelSyncLock must release it before touching the pricing cache (see
// InitChannelCache / CacheUpdateChannel), otherwise it deadlocks.
// The returned configs are pointers shared with the channel cache; they are
// replaced wholesale on update and never mutated in place, so reading them after
// RUnlock is safe.
func loadPricingAdvancedCustomConfigs(enableAbilities []AbilityWithChannel) map[int]*dto.AdvancedCustomConfig {
	channelIDs := make([]int, 0)
	seen := make(map[int]struct{})
	for _, ability := range enableAbilities {
		if ability.ChannelType != constant.ChannelTypeAdvancedCustom {
			continue
		}
		if _, exists := seen[ability.ChannelId]; exists {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	if len(channelIDs) == 0 {
		return nil
	}

	configs := make(map[int]*dto.AdvancedCustomConfig, len(channelIDs))
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		for _, channelID := range channelIDs {
			if config := channel2advancedCustomConfig[channelID]; config != nil {
				configs[channelID] = config
			}
		}
		return configs
	}

	for _, channelID := range channelIDs {
		channel, err := CacheGetChannel(channelID)
		if err != nil {
			common.SysLog(fmt.Sprintf("load advanced custom channel settings error: channel_id=%d, error=%v", channelID, err))
			continue
		}
		if channel.Type != constant.ChannelTypeAdvancedCustom {
			continue
		}
		if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
			configs[channelID] = config
		}
	}
	return configs
}

func appendPricingEndpoint(endpoints []string, endpoint string) []string {
	if endpoint == "" || common.StringsContains(endpoints, endpoint) {
		return endpoints
	}
	return append(endpoints, endpoint)
}

func updatePricing() {
	//modelRatios := common.GetModelRatios()
	enableAbilities, err := GetAllEnableAbilityWithChannels()
	if err != nil {
		common.SysLog(fmt.Sprintf("GetAllEnableAbilityWithChannels error: %v", err))
		return
	}
	// 预加载模型元数据与供应商一次，避免循环查询
	var allMeta []Model
	_ = DB.Find(&allMeta).Error
	names := make([]string, 0, len(enableAbilities))
	for _, ability := range enableAbilities {
		names = append(names, ability.Model)
	}
	metaMap := resolveModelMetadata(allMeta, names)

	// 预加载供应商
	var vendors []Vendor
	_ = DB.Find(&vendors).Error
	vendorMap := make(map[int]*Vendor)
	for i := range vendors {
		vendorMap[vendors[i].Id] = &vendors[i]
	}

	// 初始化默认供应商映射
	initDefaultVendorMapping(metaMap, vendorMap, enableAbilities)

	// 构建对前端友好的供应商列表
	vendorsList = make([]PricingVendor, 0, len(vendorMap))
	for _, v := range vendorMap {
		vendorsList = append(vendorsList, PricingVendor{
			ID:          v.Id,
			Name:        v.Name,
			Description: v.Description,
			Icon:        v.Icon,
		})
	}

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
	advancedCustomConfigs := loadPricingAdvancedCustomConfigs(enableAbilities)

	billingSources := make(map[string]map[string]map[constant.EndpointType][]string)
	legacyVideoBases := make(map[string]map[string][]map[string]jsplugin.LegacyPriceBasis)
	pluginGeneration := jsplugin.DefaultRegistry.Generation()
	pricingChannels := make(map[int]*Channel)
	taskPluginKeys := make(map[int]string)
	for _, ability := range enableAbilities {
		_, taskChannel := pluginGeneration.GetByChannelType(ability.ChannelType)
		if !taskChannel && ability.ChannelType != constant.ChannelTypeOpenRouter && ability.ChannelType != constant.ChannelTypeTaskPlugin && !slices.Contains(getPricingEndpointTypesForAbility(ability, advancedCustomConfigs), constant.EndpointTypeOpenAIVideo) {
			continue
		}
		if _, loaded := pricingChannels[ability.ChannelId]; loaded {
			continue
		}
		channel, err := CacheGetChannel(ability.ChannelId)
		pricingChannels[ability.ChannelId] = channel
		if err != nil {
			common.SysLog(fmt.Sprintf("load pricing task channel error: channel_id=%d, error=%v", ability.ChannelId, err))
			continue
		}
		taskPluginKeys[ability.ChannelId] = channel.GetSetting().TaskPluginKey
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
		legacyBasis, videoRoute, videoOnly := legacyVideoPriceBasis(pluginGeneration, pricingChannels[ability.ChannelId], ability.Model)
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
		if openRouterVideo {
			operationTypes = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
		}
		switch ability.ChannelType {
		case constant.ChannelTypeOpenAI, constant.ChannelTypeOpenRouter, constant.ChannelTypeAnthropic, constant.ChannelTypeGemini:
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

	modelSupportEndpointTypes = make(map[string][]constant.EndpointType)
	for model, endpoints := range modelSupportEndpointsStr {
		supportedEndpoints := make([]constant.EndpointType, 0)
		for _, endpointStr := range endpoints {
			endpointType := constant.EndpointType(endpointStr)
			supportedEndpoints = append(supportedEndpoints, endpointType)
		}
		modelSupportEndpointTypes[model] = supportedEndpoints
	}

	// 构建全局 supportedEndpointMap（默认 + 自定义覆盖）
	supportedEndpointMap = make(map[string]common.EndpointInfo)
	// 1. 默认端点
	for _, endpoints := range modelSupportEndpointTypes {
		for _, et := range endpoints {
			if info, ok := common.GetDefaultEndpointInfo(et); ok {
				if _, exists := supportedEndpointMap[string(et)]; !exists {
					supportedEndpointMap[string(et)] = info
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
					supportedEndpointMap[k] = common.EndpointInfo{Path: val, Method: "POST"}
				case map[string]any:
					ep := common.EndpointInfo{Method: "POST"}
					if p, ok := val["path"].(string); ok {
						ep.Path = p
					}
					if m, ok := val["method"].(string); ok {
						ep.Method = strings.ToUpper(m)
					}
					supportedEndpointMap[k] = ep
				default:
					// ignore unsupported types
				}
			}
		}
	}

	catalogOffers := make(map[string]map[string][]CatalogOffer)
	for _, ability := range enableAbilities {
		channel := pricingChannels[ability.ChannelId]
		if channel == nil || channel.Status != 1 {
			continue
		}
		if channel.Type == constant.ChannelTypeOpenRouter {
			if strings.TrimRight(channel.GetBaseURL(), "/") != "https://openrouter.ai/api" {
				continue
			}
			if channel.ParamOverride != nil && strings.TrimSpace(*channel.ParamOverride) != "" && strings.TrimSpace(*channel.ParamOverride) != "{}" {
				continue
			}
		}
		mapping := make(map[string]string)
		if err := common.UnmarshalJsonStr(channel.GetModelMapping(), &mapping); err != nil {
			continue
		}
		upstream, cyclic := followChannelModelMapping(mapping, ability.Model)
		if cyclic {
			continue
		}
		plugin, known := pluginGeneration.GetByChannelType(channel.Type)
		if channel.Type == constant.ChannelTypeTaskPlugin {
			plugin, known = pluginGeneration.Get(channel.GetSetting().TaskPluginKey)
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
			if source, known := pricingCatalog[upstream]; known {
				offers = source.Offers
			}
		}
		if len(offers) == 0 {
			continue
		}
		if catalogOffers[ability.Model] == nil {
			catalogOffers[ability.Model] = make(map[string][]CatalogOffer)
		}
		for _, offer := range offers {
			offer.ID = fmt.Sprintf("%d:%s:%s", ability.ChannelId, upstream, offer.ID)
			catalogOffers[ability.Model][ability.Group] = append(catalogOffers[ability.Model][ability.Group], offer)
		}
	}
	pricingMap = make([]Pricing, 0)
	for model, groups := range modelGroupsMap {
		pricing := Pricing{
			CatalogOffersByGroup:         catalogOffers[model],
			LegacyVideoPriceBasisByGroup: legacyVideoBases[model],
			ModelName:                    model,
			BillingSourcesByGroup:        billingSources[model],
			EnableGroup:                  groups.Items(),
			SupportedEndpointTypes:       modelSupportEndpointTypes[model],
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
		pricingMap = append(pricingMap, pricing)
	}

	// 防止大更新后数据不通用
	if len(pricingMap) > 0 {
		pricingMap[0].PricingVersion = "5a90f2b86c08bd983a9a2e6d66c255f4eaef9c4bc934386d2b6ae84ef0ff1f1f"
	}

	// 刷新缓存映射，供高并发快速查询
	modelEnableGroupsLock.Lock()
	modelEnableGroups = make(map[string][]string)
	modelQuotaTypeMap = make(map[string]int)
	for _, p := range pricingMap {
		modelEnableGroups[p.ModelName] = p.EnableGroup
		modelQuotaTypeMap[p.ModelName] = p.QuotaType
	}
	modelEnableGroupsLock.Unlock()

	lastGetPricingTime = time.Now()
	pricingRevision.Add(1)
}

// GetSupportedEndpointMap 返回全局端点到路径的映射
func GetSupportedEndpointMap() map[string]common.EndpointInfo {
	return supportedEndpointMap
}
