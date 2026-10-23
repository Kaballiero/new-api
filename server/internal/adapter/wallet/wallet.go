// Package wallet coordinates the shared wallet credit used by payments and redemption.
package wallet

import (
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// Credit applies the single ceiling predicate in the caller's transaction.
// BRIDGE: legacy payment completions still own the primitive until their extraction.
func Credit(tx *gorm.DB, userID int, quota int, updates map[string]any) error {
	return model.CreditTopUpQuota(tx, userID, quota, updates)
}

// Capacity checks the current wallet before creating a remote checkout.
func Capacity(userID int, quota int) error { return model.ValidateTopUpQuotaCapacity(userID, quota) }

// Sync credits the existing cache after commit without creating a partial user hash.
func Sync(userID int, quota int, operation string) {
	model.SyncCreditUserQuotaCache(userID, quota, operation)
}
