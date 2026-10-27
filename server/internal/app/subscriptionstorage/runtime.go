// Package subscriptionstorage assembles the shared subscription-plan cache.
package subscriptionstorage

import (
	"sync"

	"gorm.io/gorm"

	cache "github.com/QuantumNous/new-api/internal/adapter/cache/subscriptions"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/dbclock"
	"github.com/QuantumNous/new-api/model"
)

var cacheOnce = sync.OnceValue(func() *cache.Store { return cache.NewStore(func() *gorm.DB { return model.DB }) })

func Cache() *cache.Store { return cacheOnce() }

var sqlOnce = sync.OnceValue(func() storage.Store {
	return storage.NewStore(storage.Environment{
		DB: func() *gorm.DB { return model.DB }, Plans: Cache(), DatabaseTime: dbclock.Now, Accounts: accountstorage.SQL(),
	})
})

func SQL() storage.Store { return sqlOnce() }
