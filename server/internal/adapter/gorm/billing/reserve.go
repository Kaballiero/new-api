package billing

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	accountcache "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	keycache "github.com/QuantumNous/new-api/internal/adapter/cache/apikeys"
	cachequota "github.com/QuantumNous/new-api/internal/adapter/cache/quota"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	keyrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/counters"
	"gorm.io/gorm"
)

type ReserveEnvironment struct {
	DB                 func() *gorm.DB
	GetUserCache       func(int) (*accountcache.UserBase, error)
	GetTokenByKey      func(string, bool) (*keyrecord.Token, error)
	DecreaseTokenQuota func(int, string, int) error
}
type ReserveStore struct{ env ReserveEnvironment }

func NewReserveStore(env ReserveEnvironment) ReserveStore { return ReserveStore{env: env} }

func (store ReserveStore) persistUserQuotaDelta(id int, delta int) error {
	if common.BatchUpdateEnabled {
		counters.AddNewRecord(counters.BatchUpdateTypeUserQuota, id, delta)
		return nil
	}
	result := store.env.DB().Model(&accountrecord.User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", delta))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (store ReserveStore) persistTokenQuotaDelta(id int, delta int) error {
	if common.BatchUpdateEnabled {
		counters.AddNewRecord(counters.BatchUpdateTypeTokenQuota, id, delta)
		return nil
	}
	result := store.env.DB().Model(&keyrecord.Token{}).Where("id = ?", id).Updates(
		map[string]any{
			"remain_quota":  gorm.Expr("remain_quota + ?", delta),
			"used_quota":    gorm.Expr("used_quota - ?", delta),
			"accessed_time": common.GetTimestamp(),
		},
	)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (store ReserveStore) reserveUserQuotaDB(id int, quota int) (bool, error) {
	result := store.env.DB().Model(&accountrecord.User{}).
		Where("id = ? AND quota - batch_held_quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	return result.RowsAffected == 1, result.Error
}

func (store ReserveStore) reserveTokenQuotaDB(id int, quota int) (bool, error) {
	result := store.env.DB().Model(&keyrecord.Token{}).
		Where("id = ? AND remain_quota - batch_held_quota >= ?", id, quota).
		Updates(map[string]any{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

func (store ReserveStore) TryReserveUserQuota(id int, quota int) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return true, nil
	}
	if !common.RedisEnabled {
		return store.reserveUserQuotaDB(id, quota)
	}

	result, err := accountcache.CacheTryReserveUserQuota(id, int64(quota))
	if err == nil && result == cachequota.Miss {
		if _, hydrateErr := store.env.GetUserCache(id); hydrateErr == nil {
			result, err = accountcache.CacheTryReserveUserQuota(id, int64(quota))
		}
	}
	if err != nil || result == cachequota.Miss {
		if err != nil {
			common.SysLog("user quota cache reserve unavailable, falling back to database: " + err.Error())
		}
		return store.reserveUserQuotaDB(id, quota)
	}
	if result == cachequota.Insufficient {
		return false, nil
	}
	if err = store.persistUserQuotaDelta(id, -quota); err != nil {
		compensated, compensateErr := accountcache.CacheApplyUserQuotaDelta(id, int64(quota))
		if compensateErr != nil || compensated != cachequota.OK {
			common.SysError(fmt.Sprintf("failed to compensate reserved user quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}

func (store ReserveStore) TryReserveTokenQuota(id int, key string, quota int, unlimited bool) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return true, nil
	}
	if unlimited {
		return true, store.env.DecreaseTokenQuota(id, key, quota)
	}
	if !common.RedisEnabled {
		return store.reserveTokenQuotaDB(id, quota)
	}

	result, err := keycache.CacheTryReserveTokenQuota(id, key, int64(quota))
	if err == nil && result == cachequota.Miss {
		if _, hydrateErr := store.env.GetTokenByKey(key, true); hydrateErr == nil {
			result, err = keycache.CacheTryReserveTokenQuota(id, key, int64(quota))
		}
	}
	if err != nil || result == cachequota.Miss {
		if err != nil {
			common.SysLog("token quota cache reserve unavailable, falling back to database: " + err.Error())
		}
		return store.reserveTokenQuotaDB(id, quota)
	}
	if result == cachequota.Insufficient {
		return false, nil
	}
	if err = store.persistTokenQuotaDelta(id, -quota); err != nil {
		compensated, compensateErr := keycache.CacheApplyTokenQuotaDelta(id, key, int64(quota))
		if compensateErr != nil || compensated != cachequota.OK {
			common.SysError(fmt.Sprintf("failed to compensate reserved token quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}
