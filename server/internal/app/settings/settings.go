package settings

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/settings"
	runtime "github.com/QuantumNous/new-api/internal/adapter/settings/settings"
	"github.com/QuantumNous/new-api/internal/settings/sync_options"
	"github.com/QuantumNous/new-api/internal/settings/write_option"
	"github.com/QuantumNous/new-api/internal/settings/write_option_batch"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type Core struct {
	Write  write_option.Usecase
	Bulk   write_option_batch.Usecase
	Reload sync_options.Usecase
}
type diagnostics struct{}

func (diagnostics) Error(message string) { common.SysError(message) }
func (diagnostics) Log(message string)   { common.SysLog(message) }

var Default = sync.OnceValue(func() Core {
	store := storage.NewStore(func() *gorm.DB { return model.DB })
	validate := runtime.Validation{}.StoredValue
	bulk := write_option_batch.NewUsecase(store, write_option_batch.Publisher{Lock: model.LockOptionPublication, Apply: model.PublishOption,
		CanonicalGroupRatio: model.CanonicalGroupRatioOption, Aliases: model.GroupRatioOptionAliases}, validate)
	write := write_option.NewUsecase(store, write_option.Routing{
		IsBulk: func(key string) bool {
			return model.CanonicalGroupRatioOption(key) != "" || key == model.PricingCatalogOptionKey
		}, IsPricing: model.IsModelPricingOption},
		write_option.Writers{
			Bulk: func(values map[string]string) error { return bulk.Execute(context.Background(), values) }, Pricing: model.UpdateModelPricingOptions,
			Publish: model.PublishOption, Validate: validate})
	reload := sync_options.NewUsecase(store, sync_options.Publisher{Lock: model.LockOptionPublication, Apply: model.PublishOption,
		CanonicalGroupRatio: model.CanonicalGroupRatioOption}, diagnostics{})
	return Core{Write: write, Bulk: bulk, Reload: reload}
})

func Initialize() { runtime.Runtime{}.ResetDefaults(); _ = Default().Reload.Execute() }

// SyncOptions preserves sleep-before-work cadence on every node.
func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing options from database")
		_ = Default().Reload.Execute()
		model.SyncPricingCatalogueInputs()
	}
}
