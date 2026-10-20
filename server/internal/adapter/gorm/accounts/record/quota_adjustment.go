package model

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	"gorm.io/gorm"
)

// UserQuotaAdjustment сохраняет совместимость прежнего входа с нейтральным снимком сценария.

// QuotaAdjustmentStore скрывает транзакцию, кэш и текущие ошибки persistence owner.
type QuotaAdjustmentStore struct {
	db                         func() *gorm.DB
	lock                       func(*gorm.DB) *gorm.DB
	cache                      func(int, int64) error
	invalid, permission, limit func() error
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
func (s QuotaAdjustmentStore) Invalid() error    { return s.invalid() }
func (s QuotaAdjustmentStore) Permission() error { return s.permission() }
func (s QuotaAdjustmentStore) Limit() error      { return s.limit() }

type quotaAdjustmentTx struct {
	tx   *gorm.DB
	lock func(*gorm.DB) *gorm.DB
}

func (operation quotaAdjustmentTx) LockAccount(userID int) (adjust_quota.QuotaAccount, error) {
	var user User
	if err := operation.lock(operation.tx).First(&user, userID).Error; err != nil {
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
		return gorm.ErrRecordNotFound
	}
	return nil
}

func NewQuotaAdjustmentStore(db func() *gorm.DB, lock func(*gorm.DB) *gorm.DB, cache func(int, int64) error, invalid, permission, limit func() error) QuotaAdjustmentStore {
	return QuotaAdjustmentStore{db: db, lock: lock, cache: cache, invalid: invalid, permission: permission, limit: limit}
}
