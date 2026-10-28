// Package identitystorage assembles the single identity SQL and session-cache owner.
package identitystorage

import (
	"sync"

	cacheaccounts "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var shared = sync.OnceValue(func() identitystore.Store {
	return identitystore.NewStore(identitystore.Environment{
		DB:                             func() *gorm.DB { return model.DB },
		IncrementUserAuthVersionWithTx: accountstorage.Cache().IncrementUserAuthVersionWithTx,
		PublishUserAuthCache:           accountstorage.Cache().PublishUserAuthCache,
		UserCacheTTLSeconds:            cacheaccounts.UserCacheTTLSeconds,
		EnsureEmailAvailableWithTx:     accountstorage.SQL().EnsureEmailAvailableWithTx,
		LockNormalizedEmail:            accountstorage.SQL().LockNormalizedEmail,
		NormalizeEmail:                 core.NormalizeEmail,
		AllowedBindColumn:              core.AllowedBindColumn,
	})
})

func SQL() identitystore.Store { return shared() }
