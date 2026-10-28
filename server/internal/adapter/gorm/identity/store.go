// Package identity owns identity SQL transactions and the session cache.
package identity

import "gorm.io/gorm"

type Environment struct {
	DB                             func() *gorm.DB
	IncrementUserAuthVersionWithTx func(*gorm.DB, int) (int64, error)
	PublishUserAuthCache           func(int) error
	UserCacheTTLSeconds            func() int
	EnsureEmailAvailableWithTx     func(*gorm.DB, string, int) error
	LockNormalizedEmail            func(*gorm.DB, string) error
	NormalizeEmail                 func(string) string
	AllowedBindColumn              func(string) bool
}
type Store struct{ env Environment }

func NewStore(env Environment) Store { return Store{env: env} }

func (store Store) Transaction(run func(*gorm.DB) error) error {
	return store.env.DB().Transaction(run)
}
