package model

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"gorm.io/gorm"
)

func NewAccountGroupStore() record.GroupStore {
	return record.NewGroupStore(func() *gorm.DB { return DB }, RefreshUserGroupCache)
}
