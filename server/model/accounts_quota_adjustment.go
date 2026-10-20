package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"gorm.io/gorm"
)

type UserQuotaAdjustment = accounts.UserQuotaAdjustment

func AdjustUserQuota(userID, operatorRole int, mode string, value int) (*UserQuotaAdjustment, error) {
	store := NewQuotaAdjustmentStore()
	scenario := adjust_quota.NewUsecase(store, store, store, store, common.RoleRootUser)
	return scenario.Execute(userID, operatorRole, mode, value)
}

func NewQuotaAdjustmentStore() record.QuotaAdjustmentStore {
	return record.NewQuotaAdjustmentStore(func() *gorm.DB { return DB }, lockForUpdate, cacheIncrUserQuota, func() error { return ErrInvalidUserQuotaAdjustment }, func() error { return ErrUserQuotaPermission }, func() error { return ErrWalletQuotaLimitExceeded })
}
