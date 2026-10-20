package model

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"gorm.io/gorm"
)

func NewCheckinStore() record.CheckinStore {
	return record.NewCheckinStore(func() *gorm.DB { return DB }, IncreaseUserQuota, cacheIncrUserQuota)
}
