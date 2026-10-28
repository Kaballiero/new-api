package batch

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

func (Cache) ApplyEvent(id int64, key, quotaField string, ownerID int, version, debit, held int64) error {
	const script = `
if tonumber(redis.call('HGET',KEYS[1],'Id') or '0') ~= tonumber(ARGV[1]) or redis.call('HEXISTS',KEYS[1],ARGV[2]) == 0 then return -1 end
local version=tonumber(redis.call('HGET',KEYS[1],'BatchQuotaVersion') or '-1')
local incoming=tonumber(ARGV[3])
if version >= incoming then return 1 end
if version+1 ~= incoming then return -2 end
redis.call('HINCRBY',KEYS[1],ARGV[2],-tonumber(ARGV[4]))
if ARGV[2] == 'RemainQuota' then redis.call('HINCRBY',KEYS[1],'UsedQuota',tonumber(ARGV[4])) end
redis.call('HSET',KEYS[1],'BatchHeldQuota',ARGV[5],'BatchQuotaVersion',ARGV[3])
return 1`
	result, err := common.RDB.Eval(context.Background(), script, []string{key}, ownerID, quotaField, version, debit, held).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return fmt.Errorf("batch quota cache event %d awaits account version: %d", id, result)
	}
	return nil
}
