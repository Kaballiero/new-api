package catalog

import (
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	legacy "github.com/QuantumNous/new-api/model"
)

var MetadataMutationMu sync.Mutex

// MetadataTransaction serializes changes to vendors and model ownership before
// acquiring model/vendor row locks. The option anchor also covers empty tables.
func MetadataTransaction(change func(*gorm.DB) error) error {
	MetadataMutationMu.Lock()
	defer MetadataMutationMu.Unlock()
	return legacy.DB.Transaction(func(tx *gorm.DB) error {
		if err := LockMetadataMutation(tx); err != nil {
			return err
		}
		return change(tx)
	})
}

func LockMetadataMutation(tx *gorm.DB) error {
	anchor := legacy.Option{Key: "metadata_sync_lock", Value: ""}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&anchor).Error; err != nil {
		return err
	}
	return legacy.LockForUpdate(tx).Where(legacy.KeyColumn+" = ?", anchor.Key).First(&anchor).Error
}
