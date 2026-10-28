package accounts

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/cache/quota"
)

const userQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local quota = tonumber(redis.call('HGET', KEYS[1], 'Quota'))
local held = tonumber(redis.call('HGET', KEYS[1], 'BatchHeldQuota') or '0')
if quota == nil or held == nil or quota - held < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'Quota', -tonumber(ARGV[1]))
return 1`

const userQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', ARGV[1])
return 1`

func CacheTryReserveUserQuota(userID int, amount int64) (quota.Result, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaReserveScript,
		[]string{GetUserCacheKey(userID)}, amount, userID, userCacheSchemaVersion).Int()
	return quota.FromLua(result, err)
}

func CacheApplyUserQuotaDelta(userID int, delta int64) (quota.Result, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaDeltaScript,
		[]string{GetUserCacheKey(userID)}, delta, userID, userCacheSchemaVersion).Int()
	return quota.FromLua(result, err)
}
