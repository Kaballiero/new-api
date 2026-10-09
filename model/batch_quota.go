package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"gorm.io/gorm"
)

// SettleBatchItem commits the wallet debit, token counters, usage counters and
// immutable receipt together. Its hold remains until the cache debit is acked;
// a crash between DB commit and cache delivery cannot expose that money again.
func SettleBatchItem(batchID int64, customID string, quota int, details string) error {
	if quota < 0 || quota > common.MaxQuota {
		return errors.New("invalid batch item charge")
	}
	var receipt struct {
		PurchaseStatus string `json:"purchase_status"`
	}
	if err := common.UnmarshalJsonStr(details, &receipt); err != nil {
		return err
	}
	if receipt.PurchaseStatus != "calculated" && receipt.PurchaseStatus != "unbilled" {
		receipt.PurchaseStatus = "reconciliation_required"
	}
	var job BatchJob
	if err := DB.First(&job, batchID).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token Token
		if err := lockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var item BatchItem
		if err := lockForUpdate(tx).Where("batch_id = ? AND custom_id_hash = ?", batchID, fmt.Sprintf("%x", sha256.Sum256([]byte(customID)))).First(&item).Error; err != nil {
			return err
		}
		if item.BillingStatus == "settled" {
			if item.ChargedQuota != int64(quota) || item.FinancialDetails != details {
				return ErrBatchConflict
			}
			return nil
		}
		// Final upstream usage may exceed admission estimates. Debt is allowed,
		// but each persisted signed field must remain representable.
		var channel Channel
		if err := lockForUpdate(tx).First(&channel, job.ChannelID).Error; err != nil {
			return err
		}
		if int64(user.Quota) < math.MinInt32+int64(quota) || int64(user.UsedQuota) > math.MaxInt32-int64(quota) || user.RequestCount >= math.MaxInt32 ||
			token.RemainQuota < math.MinInt+quota || token.UsedQuota > math.MaxInt-quota ||
			channel.UsedQuota > math.MaxInt64-int64(quota) || job.ChargedQuota > math.MaxInt64-int64(quota) || job.SettledCount >= math.MaxInt {
			return errors.New("batch settlement arithmetic overflow")
		}
		if user.BatchHeldQuota < item.ReservedQuota || token.BatchHeldQuota < item.ReservedQuota {
			return ErrBatchConflict
		}
		if err := tx.Unscoped().Model(&user).Updates(map[string]any{"quota": gorm.Expr("quota - ?", quota), "used_quota": gorm.Expr("used_quota + ?", quota), "request_count": gorm.Expr("request_count + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&token).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota - ?", quota), "used_quota": gorm.Expr("used_quota + ?", quota), "accessed_time": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Channel{}).Where("id = ?", job.ChannelID).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
			return err
		}
		if err := tx.Model(&item).Updates(map[string]any{"charged_quota": quota, "billing_status": "settled", "financial_details": details, "purchase_status": receipt.PurchaseStatus, "cache_pending": true}).Error; err != nil {
			return err
		}
		if err := tx.Model(&job).Updates(map[string]any{"charged_quota": gorm.Expr("charged_quota + ?", quota), "settled_count": gorm.Expr("settled_count + 1")}).Error; err != nil {
			return err
		}
		if common.LogConsumeEnabled {
			var facts struct {
				Usage *dto.Usage     `json:"usage"`
				Other map[string]any `json:"other"`
			}
			if err := common.UnmarshalJsonStr(details, &facts); err != nil {
				return err
			}
			if facts.Other == nil {
				facts.Other = make(map[string]any)
			}
			facts.Other["batch_id"] = job.PublicID
			facts.Other["batch_custom_id"] = item.CustomID
			otherJSON, err := common.Marshal(facts.Other)
			if err != nil {
				return err
			}
			requestID := fmt.Sprintf("%x", sha256.Sum256([]byte(job.PublicID+":"+item.CustomID)))
			log := Log{UserId: user.Id, Username: user.Username, TokenName: token.Name, TokenId: token.Id, ChannelId: job.ChannelID, ModelName: job.Model, Group: job.UsingGroup, Quota: quota, Type: LogTypeConsume, CreatedAt: common.GetTimestamp(), RequestId: requestID, Other: string(otherJSON), Content: "Batch request"}
			if facts.Usage != nil {
				log.PromptTokens = facts.Usage.PromptTokens
				log.CompletionTokens = facts.Usage.CompletionTokens
			}
			payload, err := common.Marshal(log)
			if err != nil {
				return err
			}
			if err := tx.Create(&BatchLogEvent{ItemID: item.ID, Payload: string(payload), State: "pending"}).Error; err != nil {
				return err
			}
		}
		return saveBatchAccountEvents(tx, &user, &token, batchID, item.ID, int64(quota))
	})
}

// RaiseBatchItemReservation reserves a verified excess before debiting it.
// The caller must acknowledge the resulting cache events before settlement.
func RaiseBatchItemReservation(batchID int64, customID string, required int) error {
	if required < 0 || required > common.MaxQuota {
		return errors.New("invalid batch item reservation")
	}
	var job BatchJob
	if err := DB.First(&job, batchID).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token Token
		if err := lockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var item BatchItem
		if err := lockForUpdate(tx).Where("batch_id = ? AND custom_id_hash = ?", batchID, fmt.Sprintf("%x", sha256.Sum256([]byte(customID)))).First(&item).Error; err != nil {
			return err
		}
		if item.BillingStatus == "settled" {
			return ErrBatchConflict
		}
		delta := int64(required) - item.ReservedQuota
		if delta <= 0 {
			return nil
		}
		if user.BatchHeldQuota > common.MaxWalletQuota-delta || token.BatchHeldQuota > common.MaxWalletQuota-delta || int64(user.Quota)-user.BatchHeldQuota < delta || (!token.UnlimitedQuota && int64(token.RemainQuota)-token.BatchHeldQuota < delta) {
			return ErrBatchInsufficientQuota
		}
		user.BatchHeldQuota += delta
		token.BatchHeldQuota += delta
		if err := tx.Model(&item).Update("reserved_quota", required).Error; err != nil {
			return err
		}
		if err := tx.Model(&job).Update("reserved_quota", gorm.Expr("reserved_quota + ?", delta)).Error; err != nil {
			return err
		}
		return saveBatchAccountEvents(tx, &user, &token, batchID, 0, 0)
	})
}

func SyncBatchQuotaEvents(limit int) error {
	var events []BatchQuotaEvent
	if err := DB.Where("applied = ?", false).Order("id asc").Limit(min(max(limit, 1), 1000)).Find(&events).Error; err != nil {
		return err
	}
	var failures []error
	for _, event := range events {
		if err := applyBatchQuotaEvent(event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// applyBatchQuotaEvent delivers one owner's durable financial cache event.
func applyBatchQuotaEvent(event BatchQuotaEvent) error {
	if common.RedisEnabled {
		var key, quotaField string
		switch event.OwnerKind {
		case "user":
			var owner User
			if err := DB.Unscoped().Select("id", "deleted_at").First(&owner, event.OwnerID).Error; err != nil {
				return err
			}
			if owner.DeletedAt.Valid {
				if err := common.RDB.Del(context.Background(), getUserCacheKey(event.OwnerID)).Err(); err != nil {
					return err
				}
				return DB.Model(&BatchQuotaEvent{}).Where("id = ?", event.ID).Update("applied", true).Error
			}
			if _, err := GetUserCache(event.OwnerID); err != nil {
				return err
			}
			key, quotaField = getUserCacheKey(event.OwnerID), "Quota"
		case "token":
			var token Token
			err := DB.Unscoped().First(&token, event.OwnerID).Error
			if err != nil {
				return err
			}
			if token.DeletedAt.Valid {
				if err := common.RDB.Del(context.Background(), getTokenCacheKey(token.Key)).Err(); err != nil {
					return err
				}
				if err := DB.Model(&BatchQuotaEvent{}).Where("id = ?", event.ID).Update("applied", true).Error; err != nil {
					return err
				}
				return nil
			}
			if _, err := GetTokenByKey(token.Key, false); err != nil {
				return err
			}
			key, quotaField = getTokenCacheKey(token.Key), "RemainQuota"
		default:
			return errors.New("invalid batch quota event owner")
		}
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
		result, err := common.RDB.Eval(context.Background(), script, []string{key}, event.OwnerID, quotaField, event.Version, event.DebitQuota, event.HeldQuota).Int()
		if err != nil {
			return err
		}
		if result != 1 {
			return fmt.Errorf("batch quota cache event %d awaits account version: %d", event.ID, result)
		}
	}
	if err := DB.Model(&BatchQuotaEvent{}).Where("id = ?", event.ID).Update("applied", true).Error; err != nil {
		return err
	}
	return nil
}

func ConfirmBatchReservation(job *BatchJob) error {
	var pending int64
	if err := DB.Model(&BatchQuotaEvent{}).Where("batch_id = ? AND applied = ?", job.ID, false).Count(&pending).Error; err != nil {
		return err
	}
	if pending != 0 {
		return errors.New("batch reservation awaits cache confirmation")
	}
	var account User
	if err := DB.Unscoped().First(&account, job.UserID).Error; err != nil {
		return err
	}
	user := account.ToBaseUser()
	var err error
	var token Token
	err = DB.Unscoped().First(&token, job.TokenID).Error
	if err != nil {
		return err
	}
	if common.RedisEnabled && !account.DeletedAt.Valid {
		_, err = GetUserCache(job.UserID)
		if err != nil {
			return err
		}
		user, err = cacheGetUserBase(job.UserID)
		if err != nil {
			return err
		}
	}
	if common.RedisEnabled && !token.DeletedAt.Valid {
		cachedToken, cacheErr := cacheGetTokenByKey(token.Key)
		err = cacheErr
		if cachedToken != nil {
			token = *cachedToken
		}
		if err != nil {
			return err
		}
	}
	if int64(user.Quota) < user.BatchHeldQuota || (!token.UnlimitedQuota && int64(token.RemainQuota) < token.BatchHeldQuota) {
		return ErrBatchInsufficientQuota
	}
	return nil
}

// ReleaseSettledBatchHolds is replay-safe even if a process dies after an
// account debit but before cache delivery or hold release.
func ReleaseSettledBatchHolds(batchID int64) error {
	var job BatchJob
	if err := DB.First(&job, batchID).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token Token
		if err := lockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var pending int64
		if err := tx.Model(&BatchQuotaEvent{}).Where("batch_id = ? AND applied = ?", batchID, false).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return nil
		}
		var items []BatchItem
		if err := lockForUpdate(tx).Select("id", "reserved_quota").Where("batch_id = ? AND cache_pending = ?", batchID, true).Find(&items).Error; err != nil {
			return err
		}
		var release int64
		for _, item := range items {
			release += item.ReservedQuota
		}
		if release > user.BatchHeldQuota || release > token.BatchHeldQuota || release > job.ReservedQuota {
			return ErrBatchConflict
		}
		if len(items) > 0 {
			user.BatchHeldQuota -= release
			token.BatchHeldQuota -= release
			if err := tx.Model(&BatchItem{}).Where("batch_id = ? AND cache_pending = ?", batchID, true).Update("cache_pending", false).Error; err != nil {
				return err
			}
			if err := tx.Model(&job).Update("reserved_quota", gorm.Expr("reserved_quota - ?", release)).Error; err != nil {
				return err
			}
			if err := saveBatchAccountEvents(tx, &user, &token, batchID, 0, 0); err != nil {
				return err
			}
		}
		if job.SettledCount == job.RequestCount {
			var unresolved int64
			if err := tx.Model(&BatchItem{}).Where("batch_id = ? AND purchase_status NOT IN ?", batchID, []string{"calculated", "unbilled"}).Count(&unresolved).Error; err != nil {
				return err
			}
			purchaseStatus := "reconciliation_required"
			if unresolved == 0 || job.PurchaseStatus == "settled" {
				purchaseStatus = "settled"
			}
			return tx.Model(&job).Updates(map[string]any{"billing_status": "settled", "purchase_status": purchaseStatus}).Error
		}
		return nil
	})
}

// ConfirmBatchPurchaseCost records external procurement evidence independently
// of immutable client receipts. Replaying it never changes a wallet balance.
func ConfirmBatchPurchaseCost(publicID, costUSD, evidence string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var job BatchJob
		if err := lockForUpdate(tx).Where("public_id = ? AND deleted_at = 0", publicID).First(&job).Error; err != nil {
			return err
		}
		switch job.Status {
		case "completed", "failed", "expired", "cancelled":
		default:
			return ErrBatchConflict
		}
		if job.PurchaseStatus == "settled" {
			if job.PurchaseUSD == costUSD && job.PurchaseEvidence == evidence {
				return nil
			}
			return ErrBatchConflict
		}
		return tx.Model(&job).Updates(map[string]any{"purchase_status": "settled", "purchase_usd": costUSD, "purchase_evidence": evidence}).Error
	})
}
