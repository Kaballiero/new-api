package batch

import (
	"context"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	keycache "github.com/QuantumNous/new-api/internal/adapter/cache/apikeys"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/model"
)

type Cache struct{}

// ConfirmToken checks the mutation fence and live financial cache in
// one Redis operation. Never use the DB balance in place of a live pre-consume.
func (Cache) ConfirmToken(token model.Token) error {
	const script = `
if redis.call('HGET',KEYS[1],'CacheSchema') ~= '1' then return 0 end
if redis.call('HGET',KEYS[1],'Id') ~= ARGV[1] or redis.call('HGET',KEYS[1],'UserId') ~= ARGV[2] then return -1 end
if redis.call('HGET',KEYS[1],'BatchQuotaVersion') ~= ARGV[3] then return 0 end
if redis.call('HGET',KEYS[1],'BatchHeldQuota') ~= ARGV[4] then return -1 end
if redis.call('EXISTS',KEYS[2]) == 1 then return 0 end
if redis.call('HGET',KEYS[1],'Status') ~= ARGV[5] or redis.call('HGET',KEYS[1],'ExpiredTime') ~= ARGV[6] or redis.call('HGET',KEYS[1],'UnlimitedQuota') ~= ARGV[7] then return 0 end
local quota=tonumber(redis.call('HGET',KEYS[1],'RemainQuota'))
if not quota then return 0 end
if ARGV[7] ~= 'true' and quota < tonumber(ARGV[4]) then return -2 end
return 1`
	result, err := common.RDB.Eval(context.Background(), script, []string{keycache.GetTokenCacheKey(token.Key), keycache.GetTokenCacheFenceKey(token.Key)},
		token.Id, token.UserId, token.BatchQuotaVersion, token.BatchHeldQuota, token.Status,
		token.ExpiredTime, strconv.FormatBool(token.UnlimitedQuota)).Int()
	if err != nil {
		return fmt.Errorf("%w: token cache read failed", domain.ErrBatchReservationPending)
	}
	switch result {
	case 1:
		return nil
	case -1:
		return domain.ErrBatchReservationUnsafe
	case -2:
		return domain.ErrBatchInsufficientQuota
	default:
		return domain.ErrBatchReservationPending
	}
}
