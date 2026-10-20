package rebuild_catalogue

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	catalogdomain "github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"maps"
	"slices"
	"strings"
	"time"
)

type Store interface {
	Abilities() ([]domain.BuildAbility, error)
	Metadata() ([]catalogdomain.Model, error)
	Vendors() ([]catalogdomain.Vendor, error)
	Channels() ([]*domain.BuildChannel, error)
	InputFingerprint([]catalogdomain.Model, []catalogdomain.Vendor, []*domain.BuildChannel, []domain.BuildAbility) (string, error)
	ConfiguredCatalog(context.Context) (map[string]*catalogsync.Document, map[string]string, bool, error)
	CatalogEntry(*domain.BuildChannel, string, constant.EndpointType) (domain.PricingCatalogEntry, bool)
}
type Publication struct {
	Pricing                                         []domain.Pricing
	Vendors                                         []domain.PricingVendor
	EndpointTypes                                   map[string][]constant.EndpointType
	Endpoints                                       map[string]common.EndpointInfo
	Revision, SourceFingerprint, ChannelFingerprint string
	Generation, PluginGeneration                    uint64
}
type State interface {
	Generation() uint64
	Attempt(uint64, uint64)
	RetryAfter(int64)
	LockPublication() func()
	Publish(Publication)
}
type AliasTarget struct{ Declared, PluginKey string }
type Settings struct {
	PassThrough                                       func() bool
	GetModelPrice                                     func(string, bool) (float64, bool)
	HasConfiguredModelRatio                           func(string) bool
	GetModelRatio                                     func(string) (float64, bool, string)
	GetCompletionRatio                                func(string) float64
	GetCacheRatio, GetCreateCacheRatio, GetImageRatio func(string) (float64, bool)
	ContainsAudioRatio, ContainsAudioCompletionRatio  func(string) bool
	GetAudioRatio, GetAudioCompletionRatio            func(string) float64
	GetBillingMode                                    func(string) string
	GetBillingExpr, GetBatchBillingExpr               func(string) (string, bool)
}
type Usecase struct {
	store        Store
	state        State
	settings     Settings
	plugins      func() domain.PluginGeneration
	resolveAlias func(domain.PluginGeneration, string) (AliasTarget, bool)
	operations   *domain.Operations
	now          func() time.Time
	log          func(string)
}

func NewUsecase(store Store, state State, settings Settings, plugins func() domain.PluginGeneration, resolveAlias func(domain.PluginGeneration, string) (AliasTarget, bool), operations *domain.Operations, now func() time.Time, log func(string)) Usecase {
	return Usecase{store: store, state: state, settings: settings, plugins: plugins, resolveAlias: resolveAlias, operations: operations, now: now, log: log}
}
func (u Usecase) Execute() {
	generation := u.state.Generation()
	pluginGeneration := u.plugins()
	u.state.Attempt(generation, pluginGeneration.Number())
	published := false
	defer func() {
		if !published {
			u.state.RetryAfter(u.now().Add(30 * time.Second).UnixNano())
		}
	}()

	enableAbilities, err := u.store.Abilities()
	if err != nil {
		u.log(fmt.Sprintf("GetAllEnableAbilityWithChannels error: %v", err))
		return
	}
	// 预加载模型元数据与供应商一次，避免循环查询
	allMeta, err := u.store.Metadata()
	if err != nil {
		return
	}
	names := make([]string, 0, len(enableAbilities))
	for _, ability := range enableAbilities {
		names = append(names, ability.Model)
	}
	metaMap := catalogdomain.ResolveModelMetadata(allMeta, names)

	// 预加载供应商
	vendors, err := u.store.Vendors()
	if err != nil {
		return
	}
	channels, err := u.store.Channels()
	if err != nil {
		return
	}
	sourceFingerprint, err := u.store.InputFingerprint(allMeta, vendors, channels, enableAbilities)
	if err != nil {
		return
	}
	channelFingerprint, err := u.store.InputFingerprint(nil, nil, channels, enableAbilities)
	if err != nil {
		return
	}
	vendorMap := make(map[int]*domain.BuildVendor)
	for i := range vendors {
		vendorMap[vendors[i].Id] = &vendors[i]
	}

	domain.InitDefaultVendorMapping(metaMap, vendorMap, enableAbilities)

	nextVendors := make([]domain.PricingVendor, 0, len(vendorMap))
	for _, v := range vendorMap {
		nextVendors = append(nextVendors, domain.PricingVendor{
			ID:          v.Id,
			Name:        v.Name,
			Description: v.Description,
			Icon:        v.Icon,
		})
	}

	modelGroupsMap := make(map[string]*hosttypes.Set[string])

	for _, ability := range enableAbilities {
		groups, ok := modelGroupsMap[ability.Model]
		if !ok {
			groups = hosttypes.NewSet[string]()
			modelGroupsMap[ability.Model] = groups
		}
		groups.Add(ability.Group)
	}

	modelSupportEndpointsStr := make(map[string][]string)
	pricingChannels := make(map[int]*domain.BuildChannel, len(channels))
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
		// domain.Pricing owns a read-only source snapshot. domain.BuildChannel getters may repair and
		// save malformed rows, so decode only relevant inputs without those getters.
		var settings dto.ChannelSettings
		if channel.Setting != nil && *channel.Setting != "" {
			if err := common.UnmarshalJsonStr(*channel.Setting, &settings); err != nil {
				u.log(fmt.Sprintf("pricing channel setting invalid: channel_id=%d", channel.Id))
				return
			}
		}
		var other dto.ChannelOtherSettings
		if channel.OtherSettings != "" {
			if err := common.UnmarshalJsonStr(channel.OtherSettings, &other); err != nil {
				u.log(fmt.Sprintf("pricing channel settings invalid: channel_id=%d", channel.Id))
				return
			}
		}
		if channel.ModelMapping != nil && strings.TrimSpace(*channel.ModelMapping) != "" {
			var mapping map[string]string
			if err := common.UnmarshalJsonStr(*channel.ModelMapping, &mapping); err != nil {
				u.log(fmt.Sprintf("pricing channel mapping invalid: channel_id=%d", channel.Id))
				return
			}
		}
		serviceTierEnabled[channel.Id] = other.AllowServiceTier || settings.PassThroughBodyEnabled || u.settings.PassThrough()
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

	for _, ability := range enableAbilities {
		endpoints := modelSupportEndpointsStr[ability.Model]
		channelTypes := domain.GetPricingEndpointTypesForAbility(ability, advancedCustomConfigs)
		if billingSources[ability.Model] == nil {
			billingSources[ability.Model] = make(map[string]map[constant.EndpointType][]string)
		}
		if billingSources[ability.Model][ability.Group] == nil {
			billingSources[ability.Model][ability.Group] = make(map[constant.EndpointType][]string)
		}
		source := domain.BillingSourceConfiguredTariff
		openRouterVideo := ability.ChannelType == constant.ChannelTypeTaskPlugin && taskPluginKeys[ability.ChannelId] == "openrouter-video"
		if ability.ChannelType == constant.ChannelTypeOpenRouter || openRouterVideo {
			source = domain.BillingSourceOpenRouterActualCost
		}
		legacyBasis, videoRoute, videoOnly := domain.LegacyVideoPriceBasis(pluginGeneration, pricingChannels[ability.ChannelId], ability.Model, taskPluginKeys[ability.ChannelId])
		if videoOnly && ability.ChannelType != constant.ChannelTypeAdvancedCustom {
			channelTypes = slices.DeleteFunc(slices.Clone(channelTypes), func(endpoint constant.EndpointType) bool {
				return endpoint == constant.EndpointTypeImageGeneration
			})
		}
		if videoRoute && !slices.Contains(channelTypes, constant.EndpointTypeOpenAIVideo) {
			channelTypes = append(channelTypes, constant.EndpointTypeOpenAIVideo)
		}

		operationTypes := slices.Clone(channelTypes)
		if ability.ChannelType == constant.ChannelTypeOpenRouter {
			operationTypes = append(operationTypes, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact, constant.EndpointTypeEmbeddings, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini)
		}
		if videoRoute || openRouterVideo {
			operationTypes = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
		}
		if !videoRoute && slices.Contains([]int{constant.ChannelTypeOpenAI, constant.ChannelTypeOpenRouter, constant.ChannelTypeAnthropic, constant.ChannelTypeGemini}, ability.ChannelType) {
			operationTypes = append(operationTypes, domain.PricingEndpointBatch)
		}
		for _, endpoint := range operationTypes {
			if endpoint == constant.EndpointTypeOpenAIVideo && source == domain.BillingSourceConfiguredTariff {
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
					endpoints = domain.AppendPricingEndpoint(endpoints, k)
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

	nextSupportedEndpoints := make(map[string]common.EndpointInfo)

	for _, endpoints := range nextEndpointTypes {
		for _, et := range endpoints {
			if info, ok := common.GetDefaultEndpointInfo(et); ok {
				if _, exists := nextSupportedEndpoints[string(et)]; !exists {
					nextSupportedEndpoints[string(et)] = info
				}
			}
		}
	}

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

				}
			}
		}
	}

	upstreamDocuments, upstreamHashes, upstreamEnabled, err := u.store.ConfiguredCatalog(context.Background())
	if err != nil {
		return
	}
	upstreamRoutes := make(map[string]map[string][]domain.UpstreamCatalogRoute)
	upstreamMetadata := make(map[string]*catalogsync.Document)
	upstreamTypes := make(map[string]string)
	catalogOffers := make(map[string]map[string][]domain.CatalogOffer)
	for _, ability := range enableAbilities {
		channel := pricingChannels[ability.ChannelId]
		if channel == nil || channel.Status != 1 {
			continue
		}
		if upstreamEnabled && domain.IsOpenRouterCatalogChannel(channel) {
			upstream, resolved := domain.ResolvePricingUpstreamModel(channel, ability.Model)
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
				if !slices.Contains(sources, domain.BillingSourceOpenRouterActualCost) {
					continue
				}
				consumerEndpoints = append(consumerEndpoints, endpoint)
			}
			consumerDocument := original
			if canonical.Type == "decision" {

				consumerDocument = canonical
				consumerEndpoints = nil
				if slices.ContainsFunc(canonical.Offers, func(offer catalogsync.Offer) bool {
					mode, known := domain.CatalogOfferMode(offer, upstream)
					return offer.Scope == "endpoint" && offer.Eligibility != nil && offer.Eligibility.Eligible &&
						(offer.Operation == nil || *offer.Operation == "decisions") && known && mode == "sync"
				}) {
					consumerEndpoints = []constant.EndpointType{domain.PricingEndpointSystemOne}
				}
			}
			for _, endpoint := range consumerEndpoints {
				operation := domain.CatalogConsumerOperation(consumerDocument, upstream, endpoint)
				if operation.Operation == "" {
					continue
				}
				mode := "sync"
				if endpoint == domain.PricingEndpointBatch {
					mode = "batch"
				}
				if upstreamRoutes[ability.Model] == nil {
					upstreamRoutes[ability.Model] = make(map[string][]domain.UpstreamCatalogRoute)
				}
				upstreamRoutes[ability.Model][ability.Group] = append(upstreamRoutes[ability.Model][ability.Group], domain.UpstreamCatalogRoute{Operation: operation.Operation, Mode: mode, Endpoint: endpoint, ModelID: upstream, DocumentHash: upstreamHashes[canonical.ID], Document: canonical, ChannelID: channel.Id, ParamOverride: channel.ParamOverride, ServiceTierEnabled: serviceTierEnabled[channel.Id]})
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
		upstream, resolved := domain.ResolvePricingUpstreamModel(channel, ability.Model)
		if !resolved {
			continue
		}
		plugin, known := pluginGeneration.GetByChannelType(channel.Type)
		if channel.Type == constant.ChannelTypeTaskPlugin {
			plugin, known = pluginGeneration.Get(taskPluginKeys[channel.Id])
		}
		var offers []domain.CatalogOffer
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
			if source, known := u.store.CatalogEntry(channel, upstream, constant.EndpointTypeImageGeneration); known {
				offers = append(offers, source.Offers...)
			}
			for endpoint, sources := range billingSources[ability.Model][ability.Group] {
				if endpoint == constant.EndpointTypeImageGeneration {
					continue
				}
				if !slices.Contains(sources, domain.BillingSourceOpenRouterActualCost) {
					continue
				}
				if source, known := u.store.CatalogEntry(channel, upstream, endpoint); known {
					offers = append(offers, source.Offers...)
				}
			}
		}
		if len(offers) == 0 {
			continue
		}
		if catalogOffers[ability.Model] == nil {
			catalogOffers[ability.Model] = make(map[string][]domain.CatalogOffer)
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
	nextPricing := make([]domain.Pricing, 0)
	projectedExpressions := make(map[string][][]billingexpr.DisplayPrice)
	for model, groups := range modelGroupsMap {
		pricing := domain.Pricing{
			UpstreamCatalogRoutesByGroup: upstreamRoutes[model],
			CatalogOffersByGroup:         catalogOffers[model],
			LegacyVideoPriceBasisByGroup: legacyVideoBases[model],
			ModelName:                    model,
			BillingSourcesByGroup:        billingSources[model],
			EnableGroup:                  groups.Items(),
			SupportedEndpointTypes:       nextEndpointTypes[model],
		}

		if meta, ok := metaMap[model]; ok {

			if meta.Status != 1 {
				continue
			}
			pricing.Description = meta.Description
			pricing.Icon = meta.Icon
			pricing.Tags = meta.Tags
			pricing.VendorID = meta.VendorID
		}
		if upstream := upstreamMetadata[model]; upstream != nil {
			_, pricing.CatalogModality = domain.CatalogConsumerProfile(upstream)
			pricing.CatalogType = upstreamTypes[model]
			pricing.DisplayName = upstream.Name
			if pricing.Description == "" && upstream.Description != nil {
				pricing.Description = *upstream.Description
			}
		}
		modelPrice, findPrice := u.settings.GetModelPrice(model, false)
		pricing.PriceConfigured = findPrice || u.settings.HasConfiguredModelRatio(model)
		if findPrice {
			pricing.ModelPrice = modelPrice
			pricing.QuotaType = 1
		} else {
			modelRatio, _, _ := u.settings.GetModelRatio(model)
			pricing.ModelRatio = modelRatio
			pricing.CompletionRatio = u.settings.GetCompletionRatio(model)
			pricing.QuotaType = 0
		}
		if cacheRatio, ok := u.settings.GetCacheRatio(model); ok {
			pricing.CacheRatio = &cacheRatio
		}
		if createCacheRatio, ok := u.settings.GetCreateCacheRatio(model); ok {
			pricing.CreateCacheRatio = &createCacheRatio
		}
		if imageRatio, ok := u.settings.GetImageRatio(model); ok {
			pricing.ImageRatio = &imageRatio
		}
		if u.settings.ContainsAudioRatio(model) {
			audioRatio := u.settings.GetAudioRatio(model)
			pricing.AudioRatio = &audioRatio
		}
		if u.settings.ContainsAudioCompletionRatio(model) {
			audioCompletionRatio := u.settings.GetAudioCompletionRatio(model)
			pricing.AudioCompletionRatio = &audioCompletionRatio
		}
		if billingMode := u.settings.GetBillingMode(model); billingMode == "tiered_expr" {
			if expr, ok := u.settings.GetBillingExpr(model); ok && strings.TrimSpace(expr) != "" {
				pricing.BillingMode = billingMode
				pricing.BillingExpr = expr
			}
		} else if target, resolved := u.resolveAlias(pluginGeneration, model); resolved && target.Declared != "" {
			if tailMode := u.settings.GetBillingMode(target.Declared); tailMode == "tiered_expr" {
				if expr, ok := u.settings.GetBillingExpr(target.Declared); ok && strings.TrimSpace(expr) != "" {
					pricing.BillingMode = tailMode
					pricing.BillingExpr = expr
				}
			}
		}
		plugin, ok := pluginGeneration.GetByModel(model)
		if !ok {
			if target, resolved := u.resolveAlias(pluginGeneration, model); resolved {
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
				if !slices.Contains(sources, domain.BillingSourceConfiguredTariff) {
					continue
				}
				expression := pricing.BillingExpr
				if endpoint == domain.PricingEndpointBatch {
					expression, _ = u.settings.GetBatchBillingExpr(model)
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
		syncExpression, _ := u.settings.GetBillingExpr(model)
		if len(pricing.BillingUsageSchema) > 0 {
			syncExpression = pricing.BillingExpr
		}
		for _, endpoints := range pricing.BillingSourcesByGroup {
			for endpoint, sources := range endpoints {
				if !slices.Contains(sources, domain.BillingSourceOpenRouterActualCost) {
					continue
				}
				expression := syncExpression
				if endpoint == domain.PricingEndpointBatch {
					expression, _ = u.settings.GetBatchBillingExpr(model)
				}
				pricing.IndicativeBillingExprByEndpoint[endpoint] = expression
			}
		}
		u.operations.ProjectPricingOperations(&pricing, enableAbilities, pricingChannels, pluginGeneration, advancedCustomConfigs, taskPluginKeys)
		nextPricing = append(nextPricing, pricing)
	}

	if len(nextPricing) > 0 {
		nextPricing[0].PricingVersion = "5a90f2b86c08bd983a9a2e6d66c255f4eaef9c4bc934386d2b6ae84ef0ff1f1f"
	}

	revision, err := domain.PricingCatalogueFingerprint(nextPricing, nextVendors)
	if err != nil {
		return
	}

	unlock := u.state.LockPublication()
	defer unlock()
	if generation != u.state.Generation() || pluginGeneration.Number() != u.plugins().Number() {
		return
	}
	u.state.Publish(Publication{Pricing: nextPricing, Vendors: nextVendors, EndpointTypes: nextEndpointTypes, Endpoints: nextSupportedEndpoints, Revision: revision, Generation: generation, PluginGeneration: pluginGeneration.Number(), SourceFingerprint: sourceFingerprint, ChannelFingerprint: channelFingerprint})
	published = true
}
