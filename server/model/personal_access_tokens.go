package model

import (
	"github.com/QuantumNous/new-api/common"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/personal_access_tokens"
	"gorm.io/gorm"
)

// NewPersonalAccessTokenPersistence preserves replaceable host DB handles and lock semantics.
func NewPersonalAccessTokenPersistence() repository.Store {
	return repository.NewStore(func() *gorm.DB { return DB }, func() *gorm.DB { return LOG_DB.Model(&AuditLog{}) }, LockForUpdate, common.GetTimestamp)
}

// UpdateUserAccessToken writes only the current PAT generation fields.
func UpdateUserAccessToken(id int, token string) error {
	return NewPersonalAccessTokenPersistence().Update(id, token)
}
