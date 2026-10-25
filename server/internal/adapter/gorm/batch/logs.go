package batch

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s Store) SyncBatchConsumeLogs(limit int) error {
	var events []BatchLogEvent
	if err := s.db().Where("state <> ?", "applied").Order("id asc").Limit(min(max(limit, 1), 1000)).Find(&events).Error; err != nil {
		return err
	}
	var failures []error
	for _, event := range events {
		if err := s.deliverBatchConsumeLog(event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s Store) deliverBatchConsumeLog(event BatchLogEvent) error {
	if s.logDB() == nil {
		return errors.New("Batch log database is unavailable")
	}
	var log model.Log
	if err := common.UnmarshalJsonStr(event.Payload, &log); err != nil {
		return err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// MergeTree has no unique constraint or transaction. After an ambiguous
		// INSERT only authoritative readback is safe; never resend that INSERT.
		changed := s.db().Model(&BatchLogEvent{}).Where("id = ? AND state = ?", event.ID, "pending").Update("state", "writing")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected == 1 {
			if err := s.logDB().Create(&log).Error; err != nil {
				return err
			}
		} else {
			var count int64
			if err := s.logDB().Model(&model.Log{}).Where("request_id = ?", log.RequestId).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("Batch consume log %d awaits ClickHouse INSERT reconciliation", event.ID)
			}
		}
	} else {
		if err := s.logDB().Transaction(func(tx *gorm.DB) error {
			receipt := BatchLogReceipt{RequestID: log.RequestId}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt)
			if created.Error != nil {
				return created.Error
			}
			var stored BatchLogReceipt
			if err := model.LockForUpdate(tx).Where("request_id = ?", receipt.RequestID).First(&stored).Error; err != nil {
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
	return s.db().Model(&BatchLogEvent{}).Where("id = ?", event.ID).Update("state", "applied").Error
}
