package model

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
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
		if err := LockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token Token
		if err := LockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := LockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var settlement struct {
			Mode string `json:"settlement_mode"`
		}
		if job.BillingSnapshot != "" {
			if err := common.UnmarshalJsonStr(job.BillingSnapshot, &settlement); err != nil {
				return err
			}
		}
		if job.FinancialDetails != "" || (settlement.Mode == BatchSettlementOpenRouterJobCostV2 && !(job.UpstreamID == "" && job.SubmitState == "rejected" && quota == 0 && receipt.PurchaseStatus == "unbilled")) {
			return ErrBatchConflict
		}
		var item BatchItem
		if err := LockForUpdate(tx).Where("batch_id = ? AND custom_id_hash = ?", batchID, fmt.Sprintf("%x", sha256.Sum256([]byte(customID)))).First(&item).Error; err != nil {
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
		if err := LockForUpdate(tx).First(&channel, job.ChannelID).Error; err != nil {
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
			if err := tx.Create(&BatchLogEvent{ItemID: &item.ID, Payload: string(payload), State: "pending"}).Error; err != nil {
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
		if err := LockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token Token
		if err := LockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := LockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var item BatchItem
		if err := LockForUpdate(tx).Where("batch_id = ? AND custom_id_hash = ?", batchID, fmt.Sprintf("%x", sha256.Sum256([]byte(customID)))).First(&item).Error; err != nil {
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

var (
	ErrBatchReservationPending = errors.New("batch reservation awaits cache confirmation")
	ErrBatchDispatchDenied     = errors.New("batch dispatch authorization unavailable")
	ErrBatchReservationUnsafe  = errors.New("batch reservation ownership or holds are inconsistent")
)

func validateBatchDispatchOwner(job *BatchJob, user *User, token *Token) error {
	if user.Id != job.UserID || token.Id != job.TokenID || token.UserId != job.UserID ||
		job.ReservedQuota < 0 || user.BatchHeldQuota < job.ReservedQuota || token.BatchHeldQuota < job.ReservedQuota {
		return ErrBatchReservationUnsafe
	}
	if user.DeletedAt.Valid || token.DeletedAt.Valid || user.Status != common.UserStatusEnabled ||
		token.Status != common.TokenStatusEnabled || (token.ExpiredTime != -1 && token.ExpiredTime < common.GetTimestamp()) {
		// Revocation permits accounting for proven unsubmitted work, even when
		// the cache is cold or fenced. It must not hide known corrupt holds.
		if common.RedisEnabled {
			cached, err := cacheGetUserBase(user.Id)
			if err == nil && cached.BatchQuotaVersion == user.BatchQuotaVersion && cached.BatchHeldQuota != user.BatchHeldQuota {
				return ErrBatchReservationUnsafe
			}
			if err := cacheConfirmBatchToken(*token); errors.Is(err, ErrBatchReservationUnsafe) {
				return err
			}
		}
		return ErrBatchDispatchDenied
	}
	return nil
}

// ConfirmBatchReservation hydrates cold caches outside the dispatch transaction.
// A successful DB fallback is not permission to send: confirmBatchReservation
// reads the caches again, including their current versions and token fence.
func ConfirmBatchReservation(job *BatchJob) error {
	var user User
	if err := DB.Unscoped().First(&user, job.UserID).Error; err != nil {
		return err
	}
	var token Token
	if err := DB.Unscoped().First(&token, job.TokenID).Error; err != nil {
		return err
	}
	if job.SubmitState == "queued" || job.SubmitState == "preparing" {
		if err := validateBatchDispatchOwner(job, &user, &token); err != nil {
			return err
		}
	}
	if common.RedisEnabled {
		if !user.DeletedAt.Valid {
			if _, err := GetUserCache(user.Id); err != nil {
				return err
			}
		}
		if !token.DeletedAt.Valid {
			if _, err := GetTokenByKey(token.Key, false); err != nil {
				return err
			}
		}
	}
	return confirmBatchReservation(DB, job, &user, &token)
}

// confirmBatchReservation never hydrates: callers may hold account row locks.
func confirmBatchReservation(tx *gorm.DB, job *BatchJob, user *User, token *Token) error {
	if token.UserId != user.Id || job.UserID != user.Id || job.TokenID != token.Id ||
		job.ReservedQuota < 0 || user.BatchHeldQuota < job.ReservedQuota || token.BatchHeldQuota < job.ReservedQuota {
		return ErrBatchReservationUnsafe
	}
	var pending int64
	if err := tx.Model(&BatchQuotaEvent{}).Where("batch_id = ? AND applied = ?", job.ID, false).Count(&pending).Error; err != nil {
		return err
	}
	if pending != 0 {
		return ErrBatchReservationPending
	}
	quota := int64(user.Quota)
	if common.RedisEnabled {
		if !user.DeletedAt.Valid {
			cached, err := cacheGetUserBase(user.Id)
			if err != nil {
				return ErrBatchReservationPending
			}
			if cached.BatchQuotaVersion != user.BatchQuotaVersion || cached.AuthVersion != user.AuthVersion {
				return ErrBatchReservationPending
			}
			if cached.BatchHeldQuota != user.BatchHeldQuota {
				return ErrBatchReservationUnsafe
			}
			quota = int64(cached.Quota)
		}
		if !token.DeletedAt.Valid {
			if err := cacheConfirmBatchToken(*token); err != nil {
				return err
			}
		}
	} else if !token.UnlimitedQuota && int64(token.RemainQuota) < token.BatchHeldQuota {
		return ErrBatchInsufficientQuota
	}
	if quota < user.BatchHeldQuota {
		return ErrBatchInsufficientQuota
	}
	return nil
}

// MarkBatchSubmissionUnknown serializes dispatch authorization with account
// mutations and the durable no-replay marker. Network I/O follows commit only.
func MarkBatchSubmissionUnknown(job *BatchJob, runnerID string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := LockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token Token
		if err := LockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		var current BatchJob
		if err := LockForUpdate(tx).First(&current, job.ID).Error; err != nil {
			return err
		}
		if current.SubmitState != "preparing" || current.UpstreamID != "" || current.LeaseOwner != runnerID || current.LeaseUntil <= common.GetTimestamp() {
			return ErrBatchConflict
		}
		if err := validateBatchDispatchOwner(&current, &user, &token); err != nil {
			return err
		}
		if err := confirmBatchReservation(tx, &current, &user, &token); err != nil {
			return err
		}
		changed := tx.Model(&current).Where("submit_state = ? AND upstream_id = '' AND lease_owner = ? AND lease_until > ?", "preparing", runnerID, common.GetTimestamp()).Update("submit_state", "submission_unknown")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrBatchConflict
		}
		return nil
	})
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
		if err := LockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token Token
		if err := LockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := LockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var pending int64
		if err := tx.Model(&BatchQuotaEvent{}).Where("batch_id = ? AND applied = ?", batchID, false).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return nil
		}
		if job.FinancialDetails != "" {
			if job.BillingStatus == "reconciliation_required" {
				return nil
			}
			var receipt BatchJobCostReceipt
			if err := common.UnmarshalJsonStr(job.FinancialDetails, &receipt); err != nil {
				return err
			}
			if receipt.Version != BatchSettlementOpenRouterJobCostV2 || int64(receipt.Quota) != job.ChargedQuota || job.SettledCount != job.RequestCount {
				return ErrBatchConflict
			}
			release := job.ReservedQuota
			if release < 0 || release > user.BatchHeldQuota || release > token.BatchHeldQuota {
				return ErrBatchConflict
			}
			if job.BillingStatus == "settled" && release == 0 {
				return nil
			}
			user.BatchHeldQuota -= release
			token.BatchHeldQuota -= release
			if err := tx.Model(&BatchItem{}).Where("batch_id = ?", batchID).Updates(map[string]any{"reserved_quota": 0, "cache_pending": false}).Error; err != nil {
				return err
			}
			if err := tx.Model(&job).Updates(map[string]any{"reserved_quota": 0, "billing_status": "settled"}).Error; err != nil {
				return err
			}
			return saveBatchAccountEvents(tx, &user, &token, batchID, 0, 0)
		}
		var items []BatchItem
		if err := LockForUpdate(tx).Select("id", "reserved_quota").Where("batch_id = ? AND cache_pending = ?", batchID, true).Find(&items).Error; err != nil {
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
		if err := LockForUpdate(tx).Where("public_id = ? AND deleted_at = 0", publicID).First(&job).Error; err != nil {
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

const BatchSettlementOpenRouterJobCostV2 = "openrouter_job_cost_v2"

// BatchJobCostReceipt is the immutable, job-scoped monetary fact. No part of
// this total is attributed to individual output rows.
type BatchJobCostReceipt struct {
	Version    string                     `json:"version"`
	UpstreamID string                     `json:"upstream_id"`
	CostUSD    float64                    `json:"cost_usd"`
	Basis      common.OpenRouterCostBasis `json:"basis"`
	Quota      int                        `json:"quota"`
}

// SettleBatchJobCost commits a native final total once, with the same durable
// cache and log delivery guarantees as item settlement.
func SettleBatchJobCost(batchID int64) error {
	var job BatchJob
	if err := DB.First(&job, batchID).Error; err != nil {
		return err
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := LockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token Token
		if err := LockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := LockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var snapshot struct {
			Mode  string                      `json:"settlement_mode"`
			Basis *common.OpenRouterCostBasis `json:"openrouter_cost"`
		}
		if err := common.UnmarshalJsonStr(job.BillingSnapshot, &snapshot); err != nil {
			return fmt.Errorf("%w: invalid aggregate billing snapshot", ErrBatchConflict)
		}
		if snapshot.Mode != BatchSettlementOpenRouterJobCostV2 || snapshot.Basis == nil || snapshot.Basis.Version != BatchSettlementOpenRouterJobCostV2 {
			return ErrBatchConflict
		}
		switch job.Status {
		case "completed", "failed", "expired", "cancelled":
		default:
			return nil
		}
		if job.UpstreamID == "" || (job.SubmitState != "submitted" && job.SubmitState != "cancel_requested" && job.SubmitState != "cancel_sent") {
			return ErrBatchConflict
		}
		var usage map[string]json.RawMessage
		if err := common.UnmarshalJsonStr(job.UpstreamUsage, &usage); err != nil {
			return fmt.Errorf("%w: invalid aggregate usage", ErrBatchConflict)
		}
		cost, err := common.ValidateOpenRouterCost(usage["cost"])
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBatchConflict, err)
		}
		quota, clamp, err := snapshot.Basis.Quota(cost)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBatchConflict, err)
		}
		if clamp != nil {
			return fmt.Errorf("%w: %v", ErrBatchConflict, clamp)
		}
		receipt := BatchJobCostReceipt{Version: BatchSettlementOpenRouterJobCostV2, UpstreamID: job.UpstreamID, CostUSD: cost, Basis: *snapshot.Basis, Quota: quota}
		encoded, err := common.Marshal(receipt)
		if err != nil {
			return err
		}
		if job.FinancialDetails != "" {
			if job.FinancialDetails != string(encoded) {
				return ErrBatchConflict
			}
			return nil
		}
		if job.ChargedQuota != 0 || job.SettledCount != 0 || job.RequestCount <= 0 {
			return ErrBatchConflict
		}
		var conflicting int64
		if err := tx.Model(&BatchItem{}).Where("batch_id = ? AND (charged_quota <> 0 OR financial_details <> '' OR billing_status IN ?)", batchID, []string{"settled", "settled_at_batch"}).Count(&conflicting).Error; err != nil {
			return err
		}
		if conflicting != 0 {
			return ErrBatchConflict
		}
		var channel Channel
		if err := LockForUpdate(tx).First(&channel, job.ChannelID).Error; err != nil {
			return err
		}
		if int64(user.Quota) < math.MinInt32+int64(quota) || int64(user.UsedQuota) > math.MaxInt32-int64(quota) || job.RequestCount > math.MaxInt32 || user.RequestCount > math.MaxInt32-job.RequestCount || token.RemainQuota < math.MinInt+quota || token.UsedQuota > math.MaxInt-quota || channel.UsedQuota > math.MaxInt64-int64(quota) {
			return fmt.Errorf("%w: batch settlement arithmetic overflow", ErrBatchConflict)
		}
		if job.ReservedQuota < 0 || user.BatchHeldQuota < job.ReservedQuota || token.BatchHeldQuota < job.ReservedQuota {
			return ErrBatchConflict
		}
		if err := tx.Unscoped().Model(&user).Updates(map[string]any{"quota": gorm.Expr("quota - ?", quota), "used_quota": gorm.Expr("used_quota + ?", quota), "request_count": gorm.Expr("request_count + ?", job.RequestCount)}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&token).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota - ?", quota), "used_quota": gorm.Expr("used_quota + ?", quota), "accessed_time": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		if err := tx.Model(&channel).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
			return err
		}
		if err := tx.Model(&BatchItem{}).Where("batch_id = ?", batchID).Update("billing_status", "settled_at_batch").Error; err != nil {
			return err
		}
		if err := tx.Model(&job).Updates(map[string]any{"charged_quota": quota, "settled_count": job.RequestCount, "financial_details": string(encoded), "billing_status": "pending_cache"}).Error; err != nil {
			return err
		}
		if common.LogConsumeEnabled {
			other := new(LogOther)
			other.SetPublic("batch_id", job.PublicID)
			other.SetPublic("settlement_scope", "job")
			other.SetPublic("settlement_mode", BatchSettlementOpenRouterJobCostV2)
			other.SetPublic("actual_quota", quota)
			other.SetAdmin("upstream_cost_usd", cost)
			other.SetAdmin("openrouter_cost_basis", snapshot.Basis)
			otherJSON, err := common.Marshal(other)
			if err != nil {
				return err
			}
			requestID := fmt.Sprintf("%x", sha256.Sum256([]byte(job.PublicID+":aggregate:v2")))
			log := Log{UserId: user.Id, Username: user.Username, TokenName: token.Name, TokenId: token.Id, ChannelId: job.ChannelID, ModelName: job.Model, Group: job.UsingGroup, Quota: quota, Type: LogTypeConsume, CreatedAt: common.GetTimestamp(), RequestId: requestID, Other: string(otherJSON), Content: "Batch aggregate"}
			var tokens struct {
				Prompt     *int `json:"prompt_tokens"`
				Completion *int `json:"completion_tokens"`
			}
			if common.UnmarshalJsonStr(job.UpstreamUsage, &tokens) == nil {
				if tokens.Prompt != nil && *tokens.Prompt >= 0 && *tokens.Prompt <= math.MaxInt32 {
					log.PromptTokens = *tokens.Prompt
				}
				if tokens.Completion != nil && *tokens.Completion >= 0 && *tokens.Completion <= math.MaxInt32 {
					log.CompletionTokens = *tokens.Completion
				}
			}
			payload, err := common.Marshal(log)
			if err != nil {
				return err
			}
			if err := tx.Create(&BatchLogEvent{BatchID: &job.ID, Payload: string(payload), State: "pending"}).Error; err != nil {
				return err
			}
		}
		return saveBatchAccountEvents(tx, &user, &token, batchID, 0, int64(quota))
	})
	// Database/cache infrastructure failures remain retryable. Only invalid
	// monetary evidence or an immutable-receipt conflict requires intervention.
	if errors.Is(err, ErrBatchConflict) {
		marked := DB.Model(&BatchJob{}).Where("id = ?", batchID).Update("billing_status", "reconciliation_required").Error
		return errors.Join(err, marked)
	}
	return err
}
