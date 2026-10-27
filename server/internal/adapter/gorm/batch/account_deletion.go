package batch

import (
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// These primitives retain their existing cross-module ORM owners.
func AccountDeletionFinancialDecision(tx *gorm.DB, id int) (int64, error) {
	var unresolved int64
	err := tx.Model(&model.BatchJob{}).Where("user_id = ? AND (reserved_quota > 0 OR billing_status <> ? OR purchase_status <> ?)", id, "settled", "settled").Count(&unresolved).Error
	return unresolved, err
}
