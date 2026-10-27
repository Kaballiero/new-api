package pricingruntime

import (
	"sync"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	publication "github.com/QuantumNous/new-api/internal/adapter/settings/publication"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/get_pricing_config"
	"github.com/QuantumNous/new-api/internal/pricing/mutate_pricing_options"
	"github.com/QuantumNous/new-api/internal/pricing/update_pricing_options"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func NewPricingOptionsPersistence() store.Options {
	return store.NewOptions(store.OptionsEnvironment{
		DB:        func() *gorm.DB { return legacy.DB },
		KeyColumn: func() string { return legacy.KeyColumn },
		LockRows:  legacy.LockForUpdate,
		Lock: func() func() {
			pricingstate.ModelPricingMutationMu.Lock()
			return pricingstate.ModelPricingMutationMu.Unlock
		},
		Publish: publication.PublishOption,
	})
}

var configuration = sync.OnceValue(func() *domain.Configuration {
	return domain.NewConfiguration(domain.ConfigurationRuntime{
		Capabilities: pricingadapter.Capabilities(),
		Keys:         func() []string { return pricingstate.ModelPricingOptionKeys },
		DefaultMaps:  ratio_setting.GetDefaultPricingMaps,
		MatchingName: ratio_setting.FormatMatchingModelName,
		Builtin:      billing_setting.GetBuiltinBillingExpr,
		Builtins:     billing_setting.GetBuiltinBillingExprCopy,
		SelfUse:      func() bool { return operation_setting.SelfUseModeEnabled },
		Completion: func(name string) (float64, bool) {
			v := ratio_setting.GetCompletionRatioInfo(name)
			return v.Ratio, v.Locked
		},
		ValidateBatchExpression: billing_setting.SmokeTestExpr,
		ValidateExpression: func(name, expression string) error {
			generation := jsplugin.DefaultRegistry.Generation()
			var err error
			if plugin, ok := generation.GetByModel(name); ok {
				err = billing_setting.SmokeTestTaskExpr(expression, plugin.Meta.UsageSchema)
			} else if target, resolved := channelstore.TaskAliases.ResolveTaskModelAlias(generation, name); resolved {
				if plugin, ok := generation.Get(target.PluginKey); ok {
					err = billing_setting.SmokeTestTaskExpr(expression, plugin.Meta.UsageSchema)
				} else {
					err = billing_setting.SmokeTestExpr(expression)
				}
			} else {
				err = billing_setting.SmokeTestExpr(expression)
			}
			return err
		},
	})
})

func NewPricingConfiguration() *domain.Configuration { return configuration() }

func NewPricingUsageSchemas() func(string) map[string]domain.TariffUsageField {
	generation := jsplugin.DefaultRegistry.Generation()
	return func(name string) map[string]domain.TariffUsageField {
		if plugin, ok := generation.GetByModel(name); ok {
			return pricingadapter.UsageSchema(plugin.Meta.UsageSchema)
		}
		if target, ok := channelstore.TaskAliases.ResolveTaskModelAlias(generation, name); ok {
			if plugin, ok := generation.Get(target.PluginKey); ok {
				return pricingadapter.UsageSchema(plugin.Meta.UsageSchema)
			}
		}
		return nil
	}
}

func UpdateModelPricingOptions(updates map[string]string) error {
	return cachedUpdatePricingOptions().Execute(updates)
}

func MutateModelPricingOptions(mutate func(*gorm.DB, map[string]map[string]any) error) error {
	return cachedMutatePricingOptions().Execute(NewPricingOptionsPersistence().WithNativeMutation(mutate))
}

func IsModelPricingOption(key string) bool { return NewPricingConfiguration().IsOption(key) }

var cachedMutatePricingOptions = sync.OnceValue(func() mutate_pricing_options.Usecase {
	return mutate_pricing_options.NewUsecase(
		pricingadapter.Capabilities(),
		NewPricingConfiguration(),
		common.SysError,
		RefreshPricing,
		ratio_setting.InvalidateExposedDataCache,
	)
})

var cachedUpdatePricingOptions = sync.OnceValue(func() update_pricing_options.Usecase {
	return update_pricing_options.NewUsecase(
		pricingadapter.Capabilities(),
		NewPricingOptionsPersistence(),
		NewPricingConfiguration(),
		common.SysError,
		RefreshPricing,
		ratio_setting.InvalidateExposedDataCache,
	)
})

var ModelPricingReader = sync.OnceValue(func() get_pricing_config.Usecase {
	return get_pricing_config.NewUsecase(
		pricingadapter.Capabilities(),
		NewPricingOptionsPersistence(),
		NewPricingConfiguration(),
		NewPricingUsageSchemas,
	)
})
