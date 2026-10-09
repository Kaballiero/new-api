package model

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BatchLogEvent belongs to the same transaction as its wallet receipt.
type BatchLogEvent struct {
	ID      int64
	ItemID  int64  `gorm:"not null;uniqueIndex"`
	Payload string `gorm:"size:1048576;not null"`
	State   string `gorm:"size:32;not null;index"`
}

// BatchLogReceipt lives in the SQL log database. Its unique business key and
// consume log are committed together, including when logs use a separate DB.
type BatchLogReceipt struct {
	RequestID string `gorm:"size:64;primaryKey"`
	LogID     int    `gorm:"not null;default:0"`
}

func SyncBatchConsumeLogs(limit int) error {
	var events []BatchLogEvent
	if err := DB.Where("state <> ?", "applied").Order("id asc").Limit(min(max(limit, 1), 1000)).Find(&events).Error; err != nil {
		return err
	}
	var failures []error
	for _, event := range events {
		if err := deliverBatchConsumeLog(event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func deliverBatchConsumeLog(event BatchLogEvent) error {
	if LOG_DB == nil {
		return errors.New("Batch log database is unavailable")
	}
	var log Log
	if err := common.UnmarshalJsonStr(event.Payload, &log); err != nil {
		return err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// MergeTree has no unique constraint or transaction. After an ambiguous
		// INSERT only authoritative readback is safe; never resend that INSERT.
		changed := DB.Model(&BatchLogEvent{}).Where("id = ? AND state = ?", event.ID, "pending").Update("state", "writing")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected == 1 {
			if err := LOG_DB.Create(&log).Error; err != nil {
				return err
			}
		} else {
			var count int64
			if err := LOG_DB.Model(&Log{}).Where("request_id = ?", log.RequestId).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("Batch consume log %d awaits ClickHouse INSERT reconciliation", event.ID)
			}
		}
	} else {
		if err := LOG_DB.Transaction(func(tx *gorm.DB) error {
			receipt := BatchLogReceipt{RequestID: log.RequestId}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt)
			if created.Error != nil {
				return created.Error
			}
			var stored BatchLogReceipt
			if err := lockForUpdate(tx).Where("request_id = ?", receipt.RequestID).First(&stored).Error; err != nil {
				return err
			}
			if stored.LogID != 0 {
				return nil
			}
			if err := tx.Create(&log).Error; err != nil {
				return err
			}
			return tx.Model(&stored).Update("log_id", log.Id).Error
		}); err != nil {
			return err
		}
	}
	return DB.Model(&BatchLogEvent{}).Where("id = ?", event.ID).Update("state", "applied").Error
}
