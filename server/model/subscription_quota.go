package model

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
)

// BRIDGE: one delta body shared by billing and legacy task settlement.
// Remove together with durable task settlement (roadmap wave 5).
func PostConsumeUserSubscriptionDelta(userSubscriptionId int, delta int64) error {
	if userSubscriptionId <= 0 {
		return errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := LockForUpdate(tx).
			Where("id = ?", userSubscriptionId).
			First(&sub).Error; err != nil {
			return err
		}
		newUsed := max(sub.AmountUsed+delta, 0)
		if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
			return fmt.Errorf("subscription used exceeds total, used=%d total=%d", newUsed, sub.AmountTotal)
		}
		sub.AmountUsed = newUsed
		return tx.Save(&sub).Error
	})
}
