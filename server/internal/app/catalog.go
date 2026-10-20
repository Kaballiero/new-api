package app

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/catalog/upstream"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/apply_metadata_sync"
	"github.com/QuantumNous/new-api/internal/catalog/apply_vendor_operation"
	"github.com/QuantumNous/new-api/internal/catalog/create_model"
	"github.com/QuantumNous/new-api/internal/catalog/create_vendor"
	"github.com/QuantumNous/new-api/internal/catalog/delete_model"
	"github.com/QuantumNous/new-api/internal/catalog/delete_models_batch"
	"github.com/QuantumNous/new-api/internal/catalog/delete_vendor"
	"github.com/QuantumNous/new-api/internal/catalog/enrich_models"
	"github.com/QuantumNous/new-api/internal/catalog/get_model"
	"github.com/QuantumNous/new-api/internal/catalog/get_vendor"
	"github.com/QuantumNous/new-api/internal/catalog/list_missing_models"
	"github.com/QuantumNous/new-api/internal/catalog/list_models"
	"github.com/QuantumNous/new-api/internal/catalog/list_vendors"
	"github.com/QuantumNous/new-api/internal/catalog/preview_metadata_sync"
	"github.com/QuantumNous/new-api/internal/catalog/preview_vendor_operation"
	"github.com/QuantumNous/new-api/internal/catalog/search_models"
	"github.com/QuantumNous/new-api/internal/catalog/search_vendors"
	"github.com/QuantumNous/new-api/internal/catalog/update_model"
	"github.com/QuantumNous/new-api/internal/catalog/update_vendor"
	"github.com/QuantumNous/new-api/model"
)

type CatalogEffects struct{ Audit func(string, map[string]any) }

var catalogMetadataRuntime upstream.Runtime

func NewGetAllModelsMetaHTTP() list_models.HTTP {
	return list_models.NewHTTP(list_models.NewUsecase(newCatalogStore(), common.SysError))
}
func NewSearchModelsMetaHTTP() search_models.HTTP {
	return search_models.NewHTTP(search_models.NewUsecase(newCatalogStore(), common.SysError))
}
func NewGetModelMetaHTTP() get_model.HTTP {
	return get_model.NewHTTP(get_model.NewUsecase(newCatalogStore(), common.SysError))
}
func NewCreateModelMetaHTTP() create_model.HTTP {
	return create_model.NewHTTP(create_model.NewUsecase(newCatalogStore(), common.GetTimestamp, model.RefreshPricing))
}
func NewUpdateModelMetaHTTP() update_model.HTTP {
	return update_model.NewHTTP(update_model.NewUsecase(newCatalogStore(), common.GetTimestamp, model.RefreshPricing))
}
func NewDeleteModelMetaHTTP(effects CatalogEffects) delete_model.HTTP {
	return delete_model.NewHTTP(delete_model.NewUsecase(newCatalogStore(), model.InitChannelCache, model.RefreshPricing, func(input delete_model.Input, result delete_model.Output) {
		effects.Audit("model.delete", map[string]any{"model_ids": input.ModelIDs, "remove_from_channels": input.RemoveFromChannels, "remove_pricing": input.RemovePricing, "updated_channels": result.UpdatedChannels})
	}))
}
func NewBatchDeleteModelMetaHTTP(effects CatalogEffects) delete_models_batch.HTTP {
	return delete_models_batch.NewHTTP(delete_models_batch.NewUsecase(newCatalogStore(), model.InitChannelCache, model.RefreshPricing, func(input delete_models_batch.Input, result delete_models_batch.Output) {
		effects.Audit("model.delete_batch", map[string]any{"model_ids": input.ModelIDs, "remove_from_channels": input.RemoveFromChannels, "remove_pricing": input.RemovePricing, "updated_channels": result.UpdatedChannels})
	}))
}
func NewGetAllVendorsHTTP() list_vendors.HTTP {
	return list_vendors.NewHTTP(list_vendors.NewUsecase(newCatalogStore()))
}
func NewSearchVendorsHTTP() search_vendors.HTTP {
	return search_vendors.NewHTTP(search_vendors.NewUsecase(newCatalogStore()))
}
func NewGetVendorMetaHTTP() get_vendor.HTTP {
	return get_vendor.NewHTTP(get_vendor.NewUsecase(newCatalogStore()))
}
func NewCreateVendorMetaHTTP(effects CatalogEffects) create_vendor.HTTP {
	return create_vendor.NewHTTP(create_vendor.NewUsecase(newCatalogStore(), common.GetTimestamp, model.RefreshPricing, func(id int, name string) {
		effects.Audit("vendor.metadata.save", map[string]any{"vendor_id": id, "name": name})
	}))
}
func NewUpdateVendorMetaHTTP(effects CatalogEffects) update_vendor.HTTP {
	return update_vendor.NewHTTP(update_vendor.NewUsecase(newCatalogStore(), common.GetTimestamp, model.RefreshPricing, func(id int, name string) {
		effects.Audit("vendor.metadata.save", map[string]any{"vendor_id": id, "name": name})
	}))
}
func NewDeleteVendorMetaHTTP(effects CatalogEffects) delete_vendor.HTTP {
	return delete_vendor.NewHTTP(delete_vendor.NewUsecase(newCatalogStore(), common.GetTimestamp, model.RefreshPricing, func(input delete_vendor.Input, result delete_vendor.Output) {
		effects.Audit("vendor.metadata.delete", map[string]any{"vendor_id": input.ID})
	}))
}
func NewPreviewVendorOperationHTTP() preview_vendor_operation.HTTP {
	return preview_vendor_operation.NewHTTP(preview_vendor_operation.NewUsecase(store.NewVendorOperationStore(newCatalogStore())))
}
func NewApplyVendorOperationHTTP(effects CatalogEffects) apply_vendor_operation.HTTP {
	return apply_vendor_operation.NewHTTP(apply_vendor_operation.NewUsecase(newCatalogStore(), common.GetTimestamp, model.RefreshPricing, func(input apply_vendor_operation.Input, result apply_vendor_operation.Output) {
		effects.Audit("vendor."+input.Action, map[string]any{"source_vendor_ids": input.VendorIDs, "target_vendor_id": input.TargetVendorID, "updated_model_ids": result.UpdatedModels, "deleted_vendor_ids": result.DeletedVendors})
	}))
}
func NewGetMissingModelsHTTP() list_missing_models.HTTP {
	return list_missing_models.NewHTTP(list_missing_models.NewUsecase(newCatalogStore()))
}
func NewSyncUpstreamPreviewHTTP() preview_metadata_sync.HTTP {
	return preview_metadata_sync.NewHTTP(preview_metadata_sync.NewUsecase(newCatalogStore(), upstream.NewClient(&catalogMetadataRuntime)))
}
func NewSyncUpstreamModelsHTTP(effects CatalogEffects) apply_metadata_sync.HTTP {
	return apply_metadata_sync.NewHTTP(apply_metadata_sync.NewUsecase(newCatalogStore(), upstream.NewClient(&catalogMetadataRuntime), common.GetTimestamp, model.RefreshPricing, func(result apply_metadata_sync.Output) {
		effects.Audit("model.metadata.sync", map[string]any{"created_models": result.CreatedModels, "updated_models": result.UpdatedModels, "created_vendors": result.CreatedVendors})
	}))
}

func newCatalogStore() store.Store {
	return store.NewStore(model.NewCatalogPersistence(), store.Runtime{VendorConflict: func() error { return model.ErrVendorConflict }, SyncConflict: func() error { return model.ErrMetadataSyncConflict }, SyncFields: func() []string { return model.MetadataSyncFields }, EnabledModels: model.GetEnabledModels, QuotaTypes: model.GetModelQuotaTypes, SupportEndpoints: func(name string) []string {
		var out []string
		for _, v := range model.GetModelSupportEndpointTypes(name) {
			out = append(out, string(v))
		}
		return out
	}})
}
func NewEnrichModelsUsecase() enrich_models.Usecase {
	return enrich_models.NewUsecase(newCatalogStore(), common.SysError)
}
