package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"gorm.io/gorm"
)

// QuotaAdjustmentStore hides the transaction and the quota cache of a manual
// quota adjustment.
type QuotaAdjustmentStore struct {
	db    func() *gorm.DB
	lock  func(*gorm.DB) *gorm.DB
	cache func(int, int64) error
}

func (s QuotaAdjustmentStore) Transaction(run func(adjust_quota.QuotaAdjustmentTx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(quotaAdjustmentTx{tx: tx, lock: s.lock}) })
}
func (s QuotaAdjustmentStore) Increment(userID int, delta int64) error {
	return s.cache(userID, delta)
}
func (s QuotaAdjustmentStore) AdjustmentCacheFailed(userID int, cause error) {
	common.SysError(fmt.Sprintf("failed to sync manual quota adjustment for user %d: %s", userID, cause))
}

type quotaAdjustmentTx struct {
	tx   *gorm.DB
	lock func(*gorm.DB) *gorm.DB
}

func (operation quotaAdjustmentTx) LockAccount(userID int) (adjust_quota.QuotaAccount, error) {
	var user User
	if err := operation.lock(operation.tx).First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return adjust_quota.QuotaAccount{}, domain.ErrAccountNotFound
		}
		return adjust_quota.QuotaAccount{}, err
	}
	return adjust_quota.QuotaAccount{ID: user.Id, Username: user.Username, Role: user.Role, Status: user.Status, Quota: user.Quota}, nil
}
func (operation quotaAdjustmentTx) UpdateQuota(userID, after int) error {
	result := operation.tx.Model(&User{}).Where("id = ?", userID).Update("quota", after)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.ErrAccountNotFound
	}
	return nil
}

func NewQuotaAdjustmentStore(db func() *gorm.DB, lock func(*gorm.DB) *gorm.DB, cache func(int, int64) error) QuotaAdjustmentStore {
	return QuotaAdjustmentStore{db: db, lock: lock, cache: cache}
}
