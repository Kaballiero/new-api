// Package apikeystorage assembles the shared API-key persistence and cache owners.
package apikeystorage

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	cache "github.com/QuantumNous/new-api/internal/adapter/cache/apikeys"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

type runtime struct {
	persistence record.Store
	reader      record.KeyReader
	quota       repository.QuotaStore
}

var shared = sync.OnceValue(func() runtime {
	db := func() *gorm.DB { return model.DB }
	key := func() string { return model.KeyColumn }
	quota := repository.NewQuotaStore(repository.QuotaEnvironment{DB: db, KeyColumn: key})
	persistence := record.NewStore(db, key, cache.InvalidateTokenCacheForMutation, quota.InvalidateTokensCache)
	reader := record.NewKeyReader(db, key, locking.LockForUpdate, record.KeyCache{
		Enabled: func() bool { return common.RedisEnabled }, Get: cache.CacheGetTokenByKey, Initialize: cache.CacheInitToken,
	}, common.SysLog)
	return runtime{persistence: persistence, reader: reader, quota: quota}
})

func Persistence() record.Store    { return shared().persistence }
func Reader() record.KeyReader     { return shared().reader }
func Quota() repository.QuotaStore { return shared().quota }
