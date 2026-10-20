package model

import (
	"errors"

	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	"github.com/QuantumNous/new-api/internal/catalog/apply_metadata_records"
	"github.com/QuantumNous/new-api/internal/catalog/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var metadataMutationMu sync.Mutex

// metadataTransaction serializes changes to vendors and model ownership before
// acquiring model/vendor row locks. The option anchor also covers empty tables.
func metadataTransaction(change func(*gorm.DB) error) error {
	metadataMutationMu.Lock()
	defer metadataMutationMu.Unlock()
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockMetadataMutation(tx); err != nil {
			return err
		}
		return change(tx)
	})
}

func lockMetadataMutation(tx *gorm.DB) error {
	anchor := Option{Key: "metadata_sync_lock", Value: ""}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&anchor).Error; err != nil {
		return err
	}
	return lockForUpdate(tx).Where(commonKeyCol+" = ?", anchor.Key).First(&anchor).Error
}

var ErrMetadataSyncConflict = errors.New("metadata changed; preview again before applying")

var MetadataSyncFields = []string{"description", "icon", "tags", "vendor", "endpoints", "name_rule", "status"}

type MetadataValues = domain.MetadataValues

type MetadataSyncSelection = domain.MetadataSyncSelection

type MetadataSyncUpdate = domain.MetadataSyncUpdate

type MetadataSyncResult = domain.MetadataSyncResult

func MetadataRecordVersion(local *Model, localVendor, upstreamVendor *Vendor) string {
	return domain.MetadataRecordVersion(record.ToModel((*record.Model)(local)), record.ToVendor((*record.Vendor)(localVendor)), record.ToVendor((*record.Vendor)(upstreamVendor)))
}

func GetMetadataSyncState(db *gorm.DB) (map[string]*Model, map[string]*Vendor, error) {
	mm, vv, err := NewCatalogPersistence().SyncStateOn(db)
	if err != nil {
		return nil, nil, err
	}
	models := make(map[string]*Model, len(mm))
	vendors := make(map[string]*Vendor, len(vv))
	for k, v := range mm {
		models[k] = (*Model)(record.FromModel(v))
	}
	for k, v := range vv {
		vendors[k] = (*Vendor)(record.FromVendor(v))
	}
	return models, vendors, nil
}

func ValidateMetadataValues(values MetadataValues) error {
	return domain.ValidateMetadataValues(values)
}

// ValidateModelEndpoints accepts the existing map form (custom paths) and
// type-array form (declared protocols), but never arbitrary JSON scalars.
func ValidateModelEndpoints(raw string) error { return domain.ValidateModelEndpoints(raw) }

func ApplyMetadataSync(updates []MetadataSyncUpdate, upstreamVendors map[string]Vendor) (*MetadataSyncResult, error) {
	vendors := make(map[string]domain.Vendor, len(upstreamVendors))
	for name, v := range upstreamVendors {
		vendors[name] = *record.ToVendor((*record.Vendor)(&v))
	}
	return apply_metadata_records.NewUsecase(catalogMetadataSyncBridge{}, common.GetTimestamp, RefreshPricing).Execute(updates, vendors)
}
