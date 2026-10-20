package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/get_pricing_config"
	"github.com/QuantumNous/new-api/internal/pricing/mutate_pricing_options"
	"github.com/QuantumNous/new-api/internal/pricing/update_pricing_config"
	"github.com/QuantumNous/new-api/internal/pricing/update_pricing_options"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
	"sync"
)

type PricingValues = domain.PricingValues
type ModelPricingChange = domain.ModelPricingChange
type ModelPricingEntry = domain.ModelPricingEntry
type ModelPricingSnapshot = domain.ModelPricingSnapshot

var ErrModelPricingConflict = errors.New("model pricing changed; reload before saving")

// Lock order is stable across instances. Creating missing option rows inside
// the transaction also serializes the first write to an unconfigured database.
var modelPricingOptionKeys = []string{
	"AudioCompletionRatio", "AudioRatio", "CacheRatio", "CompletionRatio",
	"CreateCacheRatio", "ImageRatio", "ModelPrice", "ModelRatio",
	"billing_setting.batch_billing_expr", "billing_setting.billing_expr", "billing_setting.billing_mode",
}
var modelPricingMutationMu sync.Mutex

func NewPricingOptionsPersistence() store.Options {
	return store.NewOptions(store.OptionsEnvironment{DB: func() *gorm.DB { return DB }, KeyColumn: func() string { return commonKeyCol }, LockRows: lockForUpdate, Lock: func() func() { modelPricingMutationMu.Lock(); return modelPricingMutationMu.Unlock }, Publish: updateOptionMap})
}
func NewPricingConfiguration() *domain.Configuration {
	return domain.NewConfiguration(domain.ConfigurationRuntime{Keys: func() []string { return modelPricingOptionKeys }, DefaultMaps: ratio_setting.GetDefaultPricingMaps, MatchingName: ratio_setting.FormatMatchingModelName, Builtin: billing_setting.GetBuiltinBillingExpr, Builtins: billing_setting.GetBuiltinBillingExprCopy, SelfUse: func() bool { return operation_setting.SelfUseModeEnabled }, Completion: func(name string) (float64, bool) {
		v := ratio_setting.GetCompletionRatioInfo(name)
		return v.Ratio, v.Locked
	}, ValidateBatchExpression: billing_setting.SmokeTestExpr, ValidateExpression: func(name, expression string) error {
		generation := jsplugin.DefaultRegistry.Generation()
		var err error
		if plugin, ok := generation.GetByModel(name); ok {
			err = billing_setting.SmokeTestTaskExpr(expression, plugin.Meta.UsageSchema)
		} else if target, resolved := ResolveTaskModelAlias(generation, name); resolved {
			if plugin, ok := generation.Get(target.PluginKey); ok {
				err = billing_setting.SmokeTestTaskExpr(expression, plugin.Meta.UsageSchema)
			} else {
				err = billing_setting.SmokeTestExpr(expression)
			}
		} else {
			err = billing_setting.SmokeTestExpr(expression)
		}
		return err
	}})
}
func NewPricingUsageSchemas() func(string) map[string]jsplugin.UsageFieldSchema {
	generation := jsplugin.DefaultRegistry.Generation()
	return func(name string) map[string]jsplugin.UsageFieldSchema {
		if plugin, ok := generation.GetByModel(name); ok {
			return plugin.Meta.UsageSchema
		}
		if target, ok := ResolveTaskModelAlias(generation, name); ok {
			if plugin, ok := generation.Get(target.PluginKey); ok {
				return plugin.Meta.UsageSchema
			}
		}
		return nil
	}
}
func IsModelPricingOption(key string) bool            { return NewPricingConfiguration().IsOption(key) }
func ModelPricingVersion(values PricingValues) string { return domain.ModelPricingVersion(values) }
func defaultPricingMaps() map[string]map[string]any   { return NewPricingConfiguration().Defaults() }
func readModelPricingMaps(db *gorm.DB) (map[string]map[string]any, map[string]bool, []string, error) {
	rows, err := store.NewOptions(store.OptionsEnvironment{DB: func() *gorm.DB { return db }, KeyColumn: func() string { return commonKeyCol }}).ReadRows(modelPricingOptionKeys)
	if err != nil {
		return nil, nil, nil, err
	}
	return NewPricingConfiguration().DecodeRows(rows)
}
func modelPricingValues(values map[string]map[string]any, name string) PricingValues {
	return NewPricingConfiguration().Values(values, name)
}
func effectiveModelPricing(values map[string]map[string]any, name string) PricingValues {
	return NewPricingConfiguration().Effective(values, name)
}
func GetModelPricingSnapshot(names []string) (*ModelPricingSnapshot, error) {
	return get_pricing_config.NewUsecase(NewPricingOptionsPersistence(), NewPricingConfiguration(), NewPricingUsageSchemas).Execute(get_pricing_config.Input{Models: names})
}
func ValidateModelPricing(name string, values PricingValues) error {
	return NewPricingConfiguration().Validate(name, values)
}
func UpdateModelPricing(changes []ModelPricingChange) error {
	return update_pricing_config.NewUsecase(NewPricingOptionsPersistence(), NewPricingConfiguration(), func() error { return ErrModelPricingConflict }, common.SysError, RefreshPricing, ratio_setting.InvalidateExposedDataCache, func([]string) {}).Execute(changes)
}
func UpdateModelPricingOptions(updates map[string]string) error {
	return update_pricing_options.NewUsecase(NewPricingOptionsPersistence(), NewPricingConfiguration(), func() error { return ErrModelPricingConflict }, common.SysError, RefreshPricing, ratio_setting.InvalidateExposedDataCache).Execute(updates)
}
func mutateModelPricingOptions(mutate func(*gorm.DB, map[string]map[string]any) error) error {
	return mutate_pricing_options.NewUsecase(NewPricingOptionsPersistence().WithNativeMutation(mutate), NewPricingConfiguration(), func() error { return ErrModelPricingConflict }, common.SysError, RefreshPricing, ratio_setting.InvalidateExposedDataCache).Execute()
}
