package pricingruntime

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/rebuild_catalogue"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func RebuildPricingCatalogueLocked() {
	cachedRebuildCatalogue().Execute()
}

var cachedRebuildCatalogue = sync.OnceValue(func() rebuild_catalogue.Usecase {
	return rebuild_catalogue.NewUsecase(
		pricingadapter.Capabilities(),
		pricingadapter.ChannelCodec{},
		pricingstate.NewPricingCatalogueStore(LoadConfiguredCatalog),
		pricingstate.PricingBuildState{},
		rebuild_catalogue.Settings{

			PassThrough:                  func() bool { return model_setting.GetGlobalSettings().PassThroughRequestEnabled },
			GetModelPrice:                ratio_setting.GetModelPrice,
			HasConfiguredModelRatio:      ratio_setting.HasConfiguredModelRatio,
			GetModelRatio:                ratio_setting.GetModelRatio,
			GetCompletionRatio:           ratio_setting.GetCompletionRatio,
			GetCacheRatio:                ratio_setting.GetCacheRatio,
			GetCreateCacheRatio:          ratio_setting.GetCreateCacheRatio,
			GetImageRatio:                ratio_setting.GetImageRatio,
			ContainsAudioRatio:           ratio_setting.ContainsAudioRatio,
			ContainsAudioCompletionRatio: ratio_setting.ContainsAudioCompletionRatio,
			GetAudioRatio:                ratio_setting.GetAudioRatio,
			GetAudioCompletionRatio:      ratio_setting.GetAudioCompletionRatio,
			GetBillingMode:               billing_setting.GetBillingMode,
			GetBillingExpr:               billing_setting.GetBillingExpr,
			GetBatchBillingExpr:          billing_setting.GetBatchBillingExpr,
		},
		func() pricingdomain.PluginGeneration {
			return pricingadapter.NewGeneration(jsplugin.DefaultRegistry.Generation())
		},
		func(g pricingdomain.PluginGeneration, name string) (rebuild_catalogue.AliasTarget, bool) {
			v, ok := channelstore.TaskAliases.ResolveTaskModelAlias(g.(*pricingadapter.Generation).Native(), name)
			return rebuild_catalogue.AliasTarget{Declared: v.Declared, PluginKey: v.PluginKey}, ok
		},
		pricingstate.NewPricingOperations(),
		time.Now,
		common.SysLog,
	)
})
