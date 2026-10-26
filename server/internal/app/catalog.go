package app

import (
	"github.com/QuantumNous/new-api/common"
	catalogcodec "github.com/QuantumNous/new-api/internal/adapter/catalog"
	"github.com/QuantumNous/new-api/internal/adapter/catalog/upstream"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/apply_metadata_sync"
	"github.com/QuantumNous/new-api/internal/catalog/apply_vendor_operation"
	"github.com/QuantumNous/new-api/internal/catalog/create_model"
	"github.com/QuantumNous/new-api/internal/catalog/create_vendor"
	"github.com/QuantumNous/new-api/internal/catalog/delete_model"
	"github.com/QuantumNous/new-api/internal/catalog/delete_models_batch"
	"github.com/QuantumNous/new-api/internal/catalog/delete_vendor"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
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
	"github.com/gin-gonic/gin"
)

// CatalogEffects records the management audit events of catalog changes.
type CatalogEffects struct {
	Audit func(c *gin.Context, action string, details map[string]any)
}

// CatalogHTTP holds the handlers of the model and vendor catalog.
type CatalogHTTP struct {
	ListModels             list_models.HTTP
	SearchModels           search_models.HTTP
	GetModel               get_model.HTTP
	CreateModel            create_model.HTTP
	UpdateModel            update_model.HTTP
	DeleteModel            delete_model.HTTP
	DeleteModelsBatch      delete_models_batch.HTTP
	ListVendors            list_vendors.HTTP
	SearchVendors          search_vendors.HTTP
	GetVendor              get_vendor.HTTP
	CreateVendor           create_vendor.HTTP
	UpdateVendor           update_vendor.HTTP
	DeleteVendor           delete_vendor.HTTP
	PreviewVendorOperation preview_vendor_operation.HTTP
	ApplyVendorOperation   apply_vendor_operation.HTTP
	ListMissingModels      list_missing_models.HTTP
	PreviewMetadataSync    preview_metadata_sync.HTTP
	ApplyMetadataSync      apply_metadata_sync.HTTP
}

var catalogMetadataRuntime upstream.Runtime

func NewCatalogHTTP(effects CatalogEffects) CatalogHTTP {
	catalog := newCatalogStore()
	source := upstream.NewClient(&catalogMetadataRuntime)
	return CatalogHTTP{
		ListModels:   list_models.NewHTTP(list_models.NewUsecase(catalog, common.SysError)),
		SearchModels: search_models.NewHTTP(search_models.NewUsecase(catalog, common.SysError)),
		GetModel:     get_model.NewHTTP(get_model.NewUsecase(catalog, common.SysError)),
		CreateModel:  create_model.NewHTTP(create_model.NewUsecase(catalog, common.GetTimestamp, model.RefreshPricing, catalogcodec.Codec{}.DecodeEndpoints)),
		UpdateModel:  update_model.NewHTTP(update_model.NewUsecase(catalog, common.GetTimestamp, model.RefreshPricing, catalogcodec.Codec{}.DecodeEndpoints)),
		DeleteModel: delete_model.NewHTTP(delete_model.NewUsecase(catalog, model.InitChannelCache, model.RefreshPricing), func(c *gin.Context, input delete_model.Input, result delete_model.Output) {
			effects.Audit(c, "model.delete", map[string]any{"model_ids": input.ModelIDs, "remove_from_channels": input.RemoveFromChannels, "remove_pricing": input.RemovePricing, "updated_channels": result.UpdatedChannels})
		}),
		DeleteModelsBatch: delete_models_batch.NewHTTP(delete_models_batch.NewUsecase(catalog, model.InitChannelCache, model.RefreshPricing), func(c *gin.Context, input delete_models_batch.Input, result delete_models_batch.Output) {
			effects.Audit(c, "model.delete_batch", map[string]any{"model_ids": input.ModelIDs, "remove_from_channels": input.RemoveFromChannels, "remove_pricing": input.RemovePricing, "updated_channels": result.UpdatedChannels})
		}),
		ListVendors:   list_vendors.NewHTTP(list_vendors.NewUsecase(catalog, catalogcodec.Codec{}.VendorRecordVersion)),
		SearchVendors: search_vendors.NewHTTP(search_vendors.NewUsecase(catalog, catalogcodec.Codec{}.VendorRecordVersion)),
		GetVendor:     get_vendor.NewHTTP(get_vendor.NewUsecase(catalog, catalogcodec.Codec{}.VendorRecordVersion)),
		CreateVendor: create_vendor.NewHTTP(create_vendor.NewUsecase(catalog, common.GetTimestamp, model.RefreshPricing, catalogcodec.Codec{}.VendorRecordVersion), func(c *gin.Context, id int, name string) {
			effects.Audit(c, "vendor.metadata.save", map[string]any{"vendor_id": id, "name": name})
		}),
		UpdateVendor: update_vendor.NewHTTP(update_vendor.NewUsecase(catalog, common.GetTimestamp, model.RefreshPricing, catalogcodec.Codec{}.VendorRecordVersion), func(c *gin.Context, id int, name string) {
			effects.Audit(c, "vendor.metadata.save", map[string]any{"vendor_id": id, "name": name})
		}),
		DeleteVendor: delete_vendor.NewHTTP(delete_vendor.NewUsecase(catalog, common.GetTimestamp, model.RefreshPricing, catalogcodec.Codec{}), func(c *gin.Context, input delete_vendor.Input, result delete_vendor.Output) {
			effects.Audit(c, "vendor.metadata.delete", map[string]any{"vendor_id": input.ID})
		}),
		PreviewVendorOperation: preview_vendor_operation.NewHTTP(preview_vendor_operation.NewUsecase(store.NewVendorOperationStore(catalog), catalogcodec.Codec{})),
		ApplyVendorOperation: apply_vendor_operation.NewHTTP(apply_vendor_operation.NewUsecase(catalog, common.GetTimestamp, model.RefreshPricing, catalogcodec.Codec{}), func(c *gin.Context, input apply_vendor_operation.Input, result apply_vendor_operation.Output) {
			effects.Audit(c, "vendor."+input.Action, map[string]any{"source_vendor_ids": input.VendorIDs, "target_vendor_id": input.TargetVendorID, "updated_model_ids": result.UpdatedModels, "deleted_vendor_ids": result.DeletedVendors})
		}),
		ListMissingModels:   list_missing_models.NewHTTP(list_missing_models.NewUsecase(catalog)),
		PreviewMetadataSync: preview_metadata_sync.NewHTTP(preview_metadata_sync.NewUsecase(catalog, source, catalogcodec.Codec{})),
		ApplyMetadataSync: apply_metadata_sync.NewHTTP(apply_metadata_sync.NewUsecase(catalog, source, common.GetTimestamp, model.RefreshPricing, catalogcodec.Codec{}), func(c *gin.Context, result apply_metadata_sync.Output) {
			effects.Audit(c, "model.metadata.sync", map[string]any{"created_models": result.CreatedModels, "updated_models": result.UpdatedModels, "created_vendors": result.CreatedVendors})
		}),
	}
}

func newCatalogStore() store.Store {
	return store.NewStore(model.NewCatalogPersistence(), store.Runtime{SyncFields: func() []string { return domain.MetadataSyncFields }, EnabledModels: model.GetEnabledModels, QuotaTypes: model.GetModelQuotaTypes, SupportEndpoints: func(name string) []string {
		var out []string
		for _, v := range model.GetModelSupportEndpointTypes(name) {
			out = append(out, string(v))
		}
		return out
	}})
}
