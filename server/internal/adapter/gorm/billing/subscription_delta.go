package billing

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/model"
)

// postConsumeUserSubscriptionDelta keeps the legacy standalone transaction.
// The nested ledger-refund transaction is intentionally preserved (TECH_DEBT #27).
func postConsumeUserSubscriptionDelta(userSubscriptionID int, delta int64) error {
	if userSubscriptionID <= 0 {
		return errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return nil
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var sub model.UserSubscription
		if err := locking.LockForUpdate(tx).Where("id = ?", userSubscriptionID).First(&sub).Error; err != nil {
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

// AdjustTaskSubscription changes only the owner's quota on the claimed task transaction.
// The caller owns commit/rollback and must pass the same transaction as the task claim.
func AdjustTaskSubscription(tx *gorm.DB, subscriptionID, userID int, delta int64) error {
	var sub model.UserSubscription
	if err := locking.LockForUpdate(tx).Where("id = ? AND user_id = ?", subscriptionID, userID).First(&sub).Error; err != nil {
		return err
	}
	used := max(sub.AmountUsed+delta, 0)
	if sub.AmountTotal > 0 && used > sub.AmountTotal {
		return fmt.Errorf("subscription used exceeds total")
	}
	return tx.Model(&sub).Update("amount_used", used).Error
}
