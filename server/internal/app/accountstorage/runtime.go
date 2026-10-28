// Package accountstorage assembles the shared account SQL and hash/fence owners.
package accountstorage

import (
	"sync"

	cache "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

type runtime struct {
	sql   core.Store
	cache cache.Store
}

var shared = sync.OnceValue(func() runtime {
	db := func() *gorm.DB { return model.DB }
	group := func() string { return model.GroupColumn }
	queries := record.NewQueries(db, group)
	cached := cache.NewStore(cache.Environment{DB: db, GroupColumn: group, GetUserByID: queries.GetUserById, LockForUpdate: locking.LockForUpdate})
	return runtime{sql: core.NewStore(core.Environment{DB: db, GroupColumn: group, Cache: cached}), cache: cached}
})

func SQL() core.Store    { return shared().sql }
func Cache() cache.Store { return shared().cache }
