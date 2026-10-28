// Package accounts owns the single user hash, quota deltas and authentication fences.
package accounts

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"

	"gorm.io/gorm"
)

type Environment struct {
	DB            func() *gorm.DB
	GroupColumn   func() string
	GetUserByID   func(int, bool) (*record.User, error)
	LockForUpdate func(*gorm.DB) *gorm.DB
}

type Store struct{ env Environment }

func NewStore(env Environment) Store { return Store{env: env} }

func ToBaseUser(user *record.User) *UserBase {
	return &UserBase{Id: user.Id, Group: user.Group, Quota: user.Quota,
		BatchHeldQuota: user.BatchHeldQuota, BatchQuotaVersion: user.BatchQuotaVersion,
		Status: user.Status, Role: user.Role, Username: user.Username, Setting: user.Setting,
		Email: user.Email, AuthVersion: user.AuthVersion, CacheSchema: userCacheSchemaVersion}
}
