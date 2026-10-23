package redemption

import (
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/redemption/record"
	"github.com/QuantumNous/new-api/internal/redemption/domain"
	"github.com/QuantumNous/new-api/internal/redemption/redeem_code"
)

// Transaction keeps locked lookup, consumption and injected wallet credit in one database transaction.
func (s Store) Transaction(run func(tx redeem_code.Tx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(redemptionTx{db: tx, store: s}) })
}

type redemptionTx struct {
	db    *gorm.DB
	store Store
}

func (tx redemptionTx) FindLocked(key string) (domain.Code, error) {
	keyCol := "`key`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		keyCol = `"key"`
	}
	var row record.Redemption
	err := tx.store.lock(tx.db).Where(keyCol+" = ?", key).First(&row).Error
	return toCode(row), err
}

func (tx redemptionTx) MarkUsed(in domain.Redemption) (bool, error) {
	result := tx.db.Model(&record.Redemption{}).Where("id = ? AND status = ?", in.ID, in.FromStatus).
		Updates(map[string]any{"redeemed_time": in.RedeemedTime, "status": in.ToStatus, "used_user_id": in.UserID})
	return result.RowsAffected != 0, result.Error
}

func (tx redemptionTx) CreditWallet(user, quota int) error {
	return tx.store.credit(tx.db, user, quota, nil)
}
