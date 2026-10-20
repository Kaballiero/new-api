package model

import (
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/rebuild_catalogue"

	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/QuantumNous/new-api/pkg/jsplugin"

	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const (
	BillingSourceConfiguredTariff                           = "configured_tariff"
	BillingSourceOpenRouterActualCost                       = "openrouter_actual_cost"
	PricingEndpointBatch              constant.EndpointType = "openai_batch"
)

type Pricing = pricingdomain.Pricing

func legacyVideoPriceBasis(generation *jsplugin.RoutingGeneration, channel *Channel, modelName, taskPluginKey string) (map[string]jsplugin.LegacyPriceBasis, bool, bool) {
	return pricingdomain.LegacyVideoPriceBasis(pricingadapter.NewGeneration(generation), pricingBuildChannel(channel), modelName, taskPluginKey)
}

type PricingVendor = pricingdomain.PricingVendor

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

func rebuildPricingCatalogueLocked() {
	rebuild_catalogue.NewUsecase(newPricingCatalogueStore(), pricingBuildState{}, rebuild_catalogue.Settings{
		PassThrough: func() bool { return model_setting.GetGlobalSettings().PassThroughRequestEnabled }, GetModelPrice: ratio_setting.GetModelPrice, HasConfiguredModelRatio: ratio_setting.HasConfiguredModelRatio, GetModelRatio: ratio_setting.GetModelRatio, GetCompletionRatio: ratio_setting.GetCompletionRatio, GetCacheRatio: ratio_setting.GetCacheRatio, GetCreateCacheRatio: ratio_setting.GetCreateCacheRatio, GetImageRatio: ratio_setting.GetImageRatio, ContainsAudioRatio: ratio_setting.ContainsAudioRatio, ContainsAudioCompletionRatio: ratio_setting.ContainsAudioCompletionRatio, GetAudioRatio: ratio_setting.GetAudioRatio, GetAudioCompletionRatio: ratio_setting.GetAudioCompletionRatio, GetBillingMode: billing_setting.GetBillingMode, GetBillingExpr: billing_setting.GetBillingExpr, GetBatchBillingExpr: billing_setting.GetBatchBillingExpr,
	}, func() pricingdomain.PluginGeneration {
		return pricingadapter.NewGeneration(jsplugin.DefaultRegistry.Generation())
	}, func(g pricingdomain.PluginGeneration, name string) (rebuild_catalogue.AliasTarget, bool) {
		v, ok := ResolveTaskModelAlias(g.(*pricingadapter.Generation).Native(), name)
		return rebuild_catalogue.AliasTarget{Declared: v.Declared, PluginKey: v.PluginKey}, ok
	}, newPricingOperations(), time.Now, common.SysLog).Execute()
}

// GetSupportedEndpointMap 返回全局端点到路径的映射
func GetSupportedEndpointMap() map[string]common.EndpointInfo {
	snapshot := publishedPricing.Load()
	if snapshot == nil {
		return nil
	}
	return snapshot.endpoints
}
