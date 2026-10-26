package model

import (
	"sync"

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
	return LockForUpdate(tx).Where(commonKeyCol+" = ?", anchor.Key).First(&anchor).Error
}
