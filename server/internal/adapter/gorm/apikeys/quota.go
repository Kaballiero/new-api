package apikeys

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	cache "github.com/QuantumNous/new-api/internal/adapter/cache/apikeys"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/counters"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

type QuotaEnvironment struct {
	DB        func() *gorm.DB
	KeyColumn func() string
}
type QuotaStore struct{ env QuotaEnvironment }

func NewQuotaStore(env QuotaEnvironment) QuotaStore { return QuotaStore{env: env} }

func (store QuotaStore) IncreaseTokenQuota(tokenId int, key string, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota must not be negative")
	}
	if common.RedisEnabled {
		gopool.Go(func() {
			// Guarded increment: a missing hash is skipped and hydrated from the
			// database by the next read; a partial quota-only hash is never created.
			if _, err := cache.CacheApplyTokenQuotaDelta(tokenId, key, int64(quota)); err != nil {
				common.SysLog("failed to increase token quota: " + err.Error())
			}
		})
	}
	if common.BatchUpdateEnabled {
		counters.AddNewRecord(counters.BatchUpdateTypeTokenQuota, tokenId, quota)
		return nil
	}
	return counters.IncreaseTokenQuota(store.env.DB(), tokenId, quota)
}

func (store QuotaStore) DecreaseTokenQuota(id int, key string, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota must not be negative")
	}
	if common.RedisEnabled {
		gopool.Go(func() {
			if _, err := cache.CacheApplyTokenQuotaDelta(id, key, int64(-quota)); err != nil {
				common.SysLog("failed to decrease token quota: " + err.Error())
			}
		})
	}
	if common.BatchUpdateEnabled {
		counters.AddNewRecord(counters.BatchUpdateTypeTokenQuota, id, -quota)
		return nil
	}
	return store.decreaseTokenQuota(id, quota)
}

func (store QuotaStore) decreaseTokenQuota(id int, quota int) (err error) {
	err = store.env.DB().Model(&record.Token{}).Where("id = ?", id).Updates(
		map[string]any{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		},
	).Error
	return err
}

func (store QuotaStore) InvalidateUserTokensCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	if userId <= 0 {
		return errors.New("invalid user id")
	}
	var tokens []record.Token
	if err := store.env.DB().Unscoped().
		Select("id", store.env.KeyColumn()).
		Where("user_id = ?", userId).
		Find(&tokens).Error; err != nil {
		return err
	}
	return store.InvalidateTokensCache(tokens)
}

func (store QuotaStore) InvalidateTokensCache(tokens []record.Token) error {
	if !common.RedisEnabled {
		return nil
	}
	var firstErr error
	for _, t := range tokens {
		if t.Key == "" {
			continue
		}
		if err := cache.InvalidateTokenCacheForMutation(t.Key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
