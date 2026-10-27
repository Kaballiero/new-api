package controller

import (
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"

	"context"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/adapter/catalog/upstream"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"

	"github.com/QuantumNous/new-api/common"
	catalogcodec "github.com/QuantumNous/new-api/internal/adapter/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/apply_metadata_sync"
	"github.com/QuantumNous/new-api/internal/catalog/create_model"
	"github.com/QuantumNous/new-api/internal/catalog/create_vendor"
	"github.com/QuantumNous/new-api/internal/catalog/update_model"
	"github.com/QuantumNous/new-api/internal/catalog/update_vendor"

	store "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	"github.com/QuantumNous/new-api/internal/catalog/apply_vendor_operation"
	"github.com/QuantumNous/new-api/internal/catalog/delete_models_batch"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/preview_vendor_operation"
	"github.com/QuantumNous/new-api/model"
)

func catalogFixtureStore() store.Store {
	return store.NewStore(pricingruntime.NewCatalogPersistence(), store.Runtime{SyncFields: func() []string { return domain.MetadataSyncFields }, EnabledModels: channelstore.GetEnabledModels, QuotaTypes: pricingruntime.GetModelQuotaTypes, SupportEndpoints: func(name string) []string {
		var out []string
		for _, v := range pricingstate.GetModelSupportEndpointTypes(name) {
			out = append(out, string(v))
		}
		return out
	}})
}
func insertModelRecord(value *model.Model) error {
	out, err := create_model.NewUsecase(catalogFixtureStore(), common.GetTimestamp, pricingruntime.RefreshPricing, catalogcodec.Codec{}.DecodeEndpoints).Execute(create_model.Input{Value: *record.ToModel(value)})
	if err != nil {
		return err
	}
	deleted := value.DeletedAt
	*value = *record.FromModel(out)
	value.DeletedAt = deleted
	return nil
}
func updateModelRecord(value *model.Model) error {
	out, err := update_model.NewUsecase(catalogFixtureStore(), common.GetTimestamp, pricingruntime.RefreshPricing, catalogcodec.Codec{}.DecodeEndpoints).Execute(update_model.Input{Value: *record.ToModel(value)})
	if err != nil {
		return err
	}
	deleted := value.DeletedAt
	*value = *record.FromModel(out)
	value.DeletedAt = deleted
	return nil
}
func deleteModelRecord(value *model.Model) error {
	_, err := deleteCatalogModels([]int{value.Id}, false, false)
	return err
}
func insertVendorRecord(value *model.Vendor) error {
	out, err := create_vendor.NewUsecase(catalogFixtureStore(), common.GetTimestamp, pricingruntime.RefreshPricing, catalogcodec.Codec{}.VendorRecordVersion).Execute(create_vendor.Input{Value: *record.ToVendor(value)})
	if err != nil {
		return err
	}
	*value = *record.FromVendor(out)
	return nil
}
func updateVendorRecord(value *model.Vendor) error {
	out, err := update_vendor.NewUsecase(catalogFixtureStore(), common.GetTimestamp, pricingruntime.RefreshPricing, catalogcodec.Codec{}.VendorRecordVersion).Execute(update_vendor.Input{Value: *record.ToVendor(value)})
	if err != nil {
		return err
	}
	*value = *record.FromVendor(out)
	return nil
}
func deleteVendorRecord(value *model.Vendor) error { return deleteVendorRecords([]int{value.Id}) }
func catalogVendorByID(id int) (*model.Vendor, error) {
	v, err := pricingruntime.NewCatalogPersistence().GetVendorByID(id)
	if err != nil {
		return nil, err
	}
	v.Version = vendorRecordVersion(v)
	return v, nil
}
func searchCatalogVendors(keyword string, offset, limit int, association ...string) ([]*model.Vendor, int64, error) {
	values, n, err := pricingruntime.NewCatalogPersistence().SearchVendors(keyword, offset, limit, association...)
	for _, v := range values {
		v.Version = vendorRecordVersion(v)
	}
	return values, n, err
}
func vendorRecordVersion(v *model.Vendor) string {
	return (catalogcodec.Codec{}).VendorRecordVersion(record.ToVendor(v))
}
func metadataRecordVersion(v *model.Model, local, upstream *model.Vendor) string {
	return (catalogcodec.Codec{}).MetadataRecordVersion(record.ToModel(v), record.ToVendor(local), record.ToVendor(upstream))
}
func previewVendorRecords(v domain.VendorOperation) (*domain.VendorOperationPreview, error) {
	s := catalogFixtureStore()
	return preview_vendor_operation.NewUsecase(store.NewVendorOperationStore(s), catalogcodec.Codec{}).Execute(v)
}
func applyVendorRecords(v domain.VendorOperation) (*domain.VendorOperationResult, error) {
	s := catalogFixtureStore()
	return apply_vendor_operation.NewUsecase(store.NewVendorOperationStore(s), common.GetTimestamp, pricingruntime.RefreshPricing, catalogcodec.Codec{}).Execute(v)
}
func deleteVendorRecords(ids []int) error {
	result := &domain.VendorOperationResult{UpdatedModels: []int{}, DeletedVendors: []int{}}
	err := catalogFixtureStore().WithVendorMutation(func(tx domain.VendorMutation) error {
		return domain.ApplyVendorOperation(tx, domain.VendorOperation{Action: "delete", VendorIDs: ids}, result, common.GetTimestamp, catalogcodec.Codec{})
	})
	if err == nil {
		pricingruntime.RefreshPricing()
	}
	return err
}
func deleteCatalogModels(ids []int, channels, pricing bool) (domain.ModelDeleteResult, error) {
	return delete_models_batch.NewUsecase(pricingruntime.NewCatalogPersistence(), channelstore.InitChannelCache, pricingruntime.RefreshPricing).Execute(delete_models_batch.Input{ModelIDs: ids, RemoveFromChannels: channels, RemovePricing: pricing, AllowRemovePricing: true})
}
func applyMetadataRecords(updates []domain.MetadataSyncUpdate, vendors map[string]model.Vendor) (*domain.MetadataSyncResult, error) {
	source := catalogFixtureMetadataSource{values: map[string]domain.MetadataValues{}, vendors: map[string]domain.Vendor{}}
	selections := make([]domain.MetadataSyncSelection, 0, len(updates))
	for _, update := range updates {
		source.values[update.ModelName] = update.Values
		selections = append(selections, update.MetadataSyncSelection)
	}
	for name, v := range vendors {
		source.vendors[name] = *record.ToVendor(&v)
	}
	return apply_metadata_sync.NewUsecase(catalogFixtureStore(), source, common.GetTimestamp, pricingruntime.RefreshPricing, catalogcodec.Codec{}).Execute(context.Background(), apply_metadata_sync.Input{Selections: selections})
}

type catalogFixtureMetadataSource struct {
	values  map[string]domain.MetadataValues
	vendors map[string]domain.Vendor
}

func (s catalogFixtureMetadataSource) Fetch(context.Context, string) (domain.SyncUpstreamSource, map[string]domain.MetadataValues, map[string]domain.Vendor, error) {
	return domain.SyncUpstreamSource{}, s.values, s.vendors, nil
}
func enrichModels(models []*model.Model) error {
	values := record.ModelsToDomain(models)
	err := domain.EnrichModels(values, catalogFixtureStore(), common.SysError)
	for i, v := range values {
		if v == nil {
			continue
		}
		target := models[i]
		target.HasMetadata = v.HasMetadata
		target.ConfiguredChannelCount = v.ConfiguredChannelCount
		target.SquareState = v.SquareState
		target.BoundChannels = v.BoundChannels
		target.EnableGroups = v.EnableGroups
		target.SupportedEndpoints = v.SupportedEndpoints
		target.QuotaTypes = v.QuotaTypes
		target.MatchedModels = v.MatchedModels
		target.MatchedCount = v.MatchedCount
	}
	// The usecase marks failures as internal; callers get the storage cause.
	return errors.Unwrap(err)
}

type metadataSyncCandidate = domain.MetadataSyncCandidate
type upstreamModel = upstream.Model
type upstreamVendor = upstream.Vendor

func normalizeLocale(locale string) (string, bool) { return domain.NormalizeLocale(locale) }
func newHTTPClient() *http.Client                  { return upstream.NewHTTPClient() }
