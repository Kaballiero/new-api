package batch

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/batch/record"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// SettleBatchItem commits the wallet debit, token counters, usage counters and
// immutable receipt together. Its hold remains until the cache debit is acked;
// a crash between s.db() commit and cache delivery cannot expose that money again.
func (s Store) SettleBatchItem(batchID int64, customID string, quota int, details string) error {
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
	if err := s.db().First(&job, batchID).Error; err != nil {
		return err
	}
	return s.db().Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := model.LockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token model.Token
		if err := model.LockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := model.LockForUpdate(tx).First(&job, batchID).Error; err != nil {
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
		if err := domain.ValidateItemSettlement(domain.Job(job), settlement.Mode, receipt.PurchaseStatus, quota); err != nil {
			return err
		}
		var item BatchItem
		if err := model.LockForUpdate(tx).Where("batch_id = ? AND custom_id_hash = ?", batchID,
			fmt.Sprintf("%x", sha256.Sum256([]byte(customID)))).First(&item).Error; err != nil {
			return err
		}
		if item.BillingStatus == "settled" {
			if item.ChargedQuota != int64(quota) || item.FinancialDetails != details {
				return domain.ErrBatchConflict
			}
			return nil
		}
		// Final upstream usage may exceed admission estimates. Debt is allowed,
		// but each persisted signed field must remain representable.
		var channel model.Channel
		if err := model.LockForUpdate(tx).First(&channel, job.ChannelID).Error; err != nil {
			return err
		}
		if err := domain.ValidateItemCharge(domain.Job(job), domain.Item(item), accounts(user, token), channel.UsedQuota, quota); err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&user).Updates(map[string]any{"quota": gorm.Expr("quota - ?",
			quota), "used_quota": gorm.Expr("used_quota + ?", quota), "request_count": gorm.Expr("request_count + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&token).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota - ?",
			quota), "used_quota": gorm.Expr("used_quota + ?", quota), "accessed_time": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Channel{}).Where("id = ?", job.ChannelID).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
			return err
		}
		if err := tx.Model(&item).Updates(map[string]any{"charged_quota": quota, "billing_status": "settled",
			"financial_details": details, "purchase_status": receipt.PurchaseStatus, "cache_pending": true}).Error; err != nil {
			return err
		}
		if err := tx.Model(&job).Updates(map[string]any{"charged_quota": gorm.Expr("charged_quota + ?",
			quota), "settled_count": gorm.Expr("settled_count + 1")}).Error; err != nil {
			return err
		}
		if common.LogConsumeEnabled {
			payload, err := s.logs.Item(domain.Job(job), domain.LogAccount{UserID: user.Id, Username: user.Username,
				TokenID: token.Id, TokenName: token.Name}, item.CustomID, quota, details)
			if err != nil {
				return err
			}
			if err := tx.Create(&BatchLogEvent{ItemID: &item.ID, Payload: payload, State: "pending"}).Error; err != nil {
				return err
			}
		}
		return s.saveBatchAccountEvents(tx, &user, &token, batchID, item.ID, int64(quota))
	})
}

func (s Store) SyncBatchQuotaEvents(limit int) error {
	var events []BatchQuotaEvent
	if err := s.db().Where("applied = ?", false).Order("id asc").Limit(min(max(limit, 1), 1000)).Find(&events).Error; err != nil {
		return err
	}
	var failures []error
	for _, event := range events {
		if err := s.applyBatchQuotaEvent(event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// applyBatchQuotaEvent delivers one owner's durable financial cache event.
func (s Store) applyBatchQuotaEvent(event BatchQuotaEvent) error {
	if common.RedisEnabled {
		var key, quotaField string
		switch event.OwnerKind {
		case "user":
			var owner model.User
			if err := s.db().Unscoped().Select("id", "deleted_at").First(&owner, event.OwnerID).Error; err != nil {
				return err
			}
			if owner.DeletedAt.Valid {
				if err := common.RDB.Del(context.Background(), model.GetUserCacheKey(event.OwnerID)).Err(); err != nil {
					return err
				}
				return s.db().Model(&BatchQuotaEvent{}).Where("id = ?", event.ID).Update("applied", true).Error
			}
			if _, err := model.GetUserCache(event.OwnerID); err != nil {
				return err
			}
			key, quotaField = model.GetUserCacheKey(event.OwnerID), "Quota"
		case "token":
			var token model.Token
			err := s.db().Unscoped().First(&token, event.OwnerID).Error
			if err != nil {
				return err
			}
			if token.DeletedAt.Valid {
				if err := common.RDB.Del(context.Background(), model.GetTokenCacheKey(token.Key)).Err(); err != nil {
					return err
				}
				if err := s.db().Model(&BatchQuotaEvent{}).Where("id = ?", event.ID).Update("applied", true).Error; err != nil {
					return err
				}
				return nil
			}
			if _, err := model.GetTokenByKey(token.Key, false); err != nil {
				return err
			}
			key, quotaField = model.GetTokenCacheKey(token.Key), "RemainQuota"
		default:
			return errors.New("invalid batch quota event owner")
		}
		if err := s.cache.ApplyEvent(event.ID, key, quotaField, event.OwnerID, event.Version, event.DebitQuota, event.HeldQuota); err != nil {
			return err
		}
	}
	if err := s.db().Model(&BatchQuotaEvent{}).Where("id = ?", event.ID).Update("applied", true).Error; err != nil {
		return err
	}
	return nil
}

func (s Store) validateBatchDispatchOwner(job *BatchJob, user *model.User, token *model.Token) error {
	if err := domain.ValidateReservationOwnership(domain.Job(*job), accounts(*user, *token)); err != nil {
		return err
	}
	if domain.DispatchRevoked(accounts(*user, *token), common.GetTimestamp(), common.UserStatusEnabled, common.TokenStatusEnabled) {
		// Revocation permits accounting for proven unsubmitted work, even when
		// the cache is cold or fenced. It must not hide known corrupt holds.
		if common.RedisEnabled {
			cached, err := model.CacheGetUserBase(user.Id)
			if err == nil && cached.BatchQuotaVersion == user.BatchQuotaVersion && cached.BatchHeldQuota != user.BatchHeldQuota {
				return domain.ErrBatchReservationUnsafe
			}
			if err := s.cache.ConfirmToken(*token); errors.Is(err, domain.ErrBatchReservationUnsafe) {
				return err
			}
		}
		return domain.ErrBatchDispatchDenied
	}
	return nil
}

// ConfirmBatchReservation hydrates cold caches outside the dispatch transaction.
// A successful s.db() fallback is not permission to send: confirmBatchReservation
// reads the caches again, including their current versions and token fence.
func (s Store) ConfirmBatchReservation(value *domain.Job) error {
	stored := record.BatchJob(*value)
	job := &stored
	var user model.User
	if err := s.db().Unscoped().First(&user, job.UserID).Error; err != nil {
		return err
	}
	var token model.Token
	if err := s.db().Unscoped().First(&token, job.TokenID).Error; err != nil {
		return err
	}
	if job.SubmitState == "queued" || job.SubmitState == "preparing" {
		if err := s.validateBatchDispatchOwner(job, &user, &token); err != nil {
			return err
		}
	}
	if common.RedisEnabled {
		if !user.DeletedAt.Valid {
			if _, err := model.GetUserCache(user.Id); err != nil {
				return err
			}
		}
		if !token.DeletedAt.Valid {
			if _, err := model.GetTokenByKey(token.Key, false); err != nil {
				return err
			}
		}
	}
	return s.confirmBatchReservation(s.db(), job, &user, &token)
}

// confirmBatchReservation never hydrates: callers may hold account row locks.
func (s Store) confirmBatchReservation(tx *gorm.DB, job *BatchJob, user *model.User, token *model.Token) error {
	if err := domain.ValidateReservationOwnership(domain.Job(*job), accounts(*user, *token)); err != nil {
		return err
	}
	var pending int64
	if err := tx.Model(&BatchQuotaEvent{}).Where("batch_id = ? AND applied = ?", job.ID, false).Count(&pending).Error; err != nil {
		return err
	}
	if pending != 0 {
		return domain.ErrBatchReservationPending
	}
	quota := int64(user.Quota)
	if common.RedisEnabled {
		if !user.DeletedAt.Valid {
			cached, err := model.CacheGetUserBase(user.Id)
			if err != nil {
				return domain.ErrBatchReservationPending
			}
			if cached.BatchQuotaVersion != user.BatchQuotaVersion || cached.AuthVersion != user.AuthVersion {
				return domain.ErrBatchReservationPending
			}
			if cached.BatchHeldQuota != user.BatchHeldQuota {
				return domain.ErrBatchReservationUnsafe
			}
			quota = int64(cached.Quota)
		}
		if !token.DeletedAt.Valid {
			if err := s.cache.ConfirmToken(*token); err != nil {
				return err
			}
		}
	} else if !token.UnlimitedQuota && int64(token.RemainQuota) < token.BatchHeldQuota {
		return domain.ErrBatchInsufficientQuota
	}
	if quota < user.BatchHeldQuota {
		return domain.ErrBatchInsufficientQuota
	}
	return nil
}

// MarkBatchSubmissionUnknown serializes dispatch authorization with account
// mutations and the durable no-replay marker. Network I/O follows commit only.
func (s Store) MarkBatchSubmissionUnknown(value *domain.Job, runnerID string) error {
	stored := record.BatchJob(*value)
	job := &stored
	return s.db().Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := model.LockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token model.Token
		if err := model.LockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		var current BatchJob
		if err := model.LockForUpdate(tx).First(&current, job.ID).Error; err != nil {
			return err
		}
		if !domain.CanMarkSubmissionUnknown(domain.Job(current), runnerID, common.GetTimestamp()) {
			return domain.ErrBatchConflict
		}
		if err := s.validateBatchDispatchOwner(&current, &user, &token); err != nil {
			return err
		}
		if err := s.confirmBatchReservation(tx, &current, &user, &token); err != nil {
			return err
		}
		changed := tx.Model(&current).Where("submit_state = ? AND upstream_id = '' AND lease_owner = ? AND lease_until > ?",
			"preparing", runnerID, common.GetTimestamp()).Update("submit_state", "submission_unknown")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return domain.ErrBatchConflict
		}
		return nil
	})
}

// ReleaseSettledBatchHolds is replay-safe even if a process dies after an
// account debit but before cache delivery or hold release.
func (s Store) ReleaseSettledBatchHolds(batchID int64) error {
	var job BatchJob
	if err := s.db().First(&job, batchID).Error; err != nil {
		return err
	}
	return s.db().Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := model.LockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token model.Token
		if err := model.LockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := model.LockForUpdate(tx).First(&job, batchID).Error; err != nil {
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
			if receipt.Version != domain.BatchSettlementOpenRouterJobCostV2 || int64(receipt.Quota) != job.ChargedQuota ||
				job.SettledCount != job.RequestCount {
				return domain.ErrBatchConflict
			}
			release := job.ReservedQuota
			if release < 0 || release > user.BatchHeldQuota || release > token.BatchHeldQuota {
				return domain.ErrBatchConflict
			}
			if job.BillingStatus == "settled" && release == 0 {
				return nil
			}
			user.BatchHeldQuota -= release
			token.BatchHeldQuota -= release
			if err := tx.Model(&BatchItem{}).Where("batch_id = ?", batchID).Updates(map[string]any{"reserved_quota": 0,
				"cache_pending": false}).Error; err != nil {
				return err
			}
			if err := tx.Model(&job).Updates(map[string]any{"reserved_quota": 0, "billing_status": "settled"}).Error; err != nil {
				return err
			}
			return s.saveBatchAccountEvents(tx, &user, &token, batchID, 0, 0)
		}
		var items []BatchItem
		if err := model.LockForUpdate(tx).Select("id", "reserved_quota").Where("batch_id = ? AND cache_pending = ?",
			batchID, true).Find(&items).Error; err != nil {
			return err
		}
		var release int64
		for _, item := range items {
			release += item.ReservedQuota
		}
		if release > user.BatchHeldQuota || release > token.BatchHeldQuota || release > job.ReservedQuota {
			return domain.ErrBatchConflict
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
			if err := s.saveBatchAccountEvents(tx, &user, &token, batchID, 0, 0); err != nil {
				return err
			}
		}
		if job.SettledCount == job.RequestCount {
			var unresolved int64
			if err := tx.Model(&BatchItem{}).Where("batch_id = ? AND purchase_status NOT IN ?",
				batchID, []string{"calculated", "unbilled"}).Count(&unresolved).Error; err != nil {
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
func (s Store) ConfirmBatchPurchaseCost(publicID, costUSD, evidence string) error {
	return s.db().Transaction(func(tx *gorm.DB) error {
		var job BatchJob
		if err := model.LockForUpdate(tx).Where("public_id = ? AND deleted_at = 0", publicID).First(&job).Error; err != nil {
			return err
		}
		switch job.Status {
		case "completed", "failed", "expired", "cancelled":
		default:
			return domain.ErrBatchConflict
		}
		if job.PurchaseStatus == "settled" {
			if job.PurchaseUSD == costUSD && job.PurchaseEvidence == evidence {
				return nil
			}
			return domain.ErrBatchConflict
		}
		return tx.Model(&job).Updates(map[string]any{"purchase_status": "settled", "purchase_usd": costUSD, "purchase_evidence": evidence}).Error
	})
}

// SettleBatchJobCost commits a native final total once, with the same durable
// cache and log delivery guarantees as item settlement.
func (s Store) SettleBatchJobCost(batchID int64) error {
	var job BatchJob
	if err := s.db().First(&job, batchID).Error; err != nil {
		return err
	}
	err := s.db().Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := model.LockForUpdate(tx.Unscoped()).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token model.Token
		if err := model.LockForUpdate(tx.Unscoped()).First(&token, job.TokenID).Error; err != nil {
			return err
		}
		if err := model.LockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var snapshot struct {
			Mode  string                      `json:"settlement_mode"`
			Basis *common.OpenRouterCostBasis `json:"openrouter_cost"`
		}
		if err := common.UnmarshalJsonStr(job.BillingSnapshot, &snapshot); err != nil {
			return fmt.Errorf("%w: invalid aggregate billing snapshot", domain.ErrBatchConflict)
		}
		var basis *billingdomain.ProviderBasis
		if snapshot.Basis != nil {
			basis = &billingdomain.ProviderBasis{Version: snapshot.Basis.Version, EffectiveGroupRatio: snapshot.Basis.EffectiveGroupRatio,
				QuotaPerUnit: snapshot.Basis.QuotaPerUnit, EstimatedUSD: snapshot.Basis.EstimatedUSD}
		}
		if err := domain.ValidateAggregateSnapshot(snapshot.Mode, basis); err != nil {
			return err
		}
		ready, err := domain.AggregateDispatchReady(domain.Job(job))
		if err != nil {
			return err
		}
		if !ready {
			return nil
		}
		var usage map[string]json.RawMessage
		if err := common.UnmarshalJsonStr(job.UpstreamUsage, &usage); err != nil {
			return fmt.Errorf("%w: invalid aggregate usage", domain.ErrBatchConflict)
		}
		cost, quota, err := domain.AggregateQuota(s.math, *basis, usage["cost"])
		if err != nil {
			return err
		}
		receipt := BatchJobCostReceipt{Version: domain.BatchSettlementOpenRouterJobCostV2, UpstreamID: job.UpstreamID,
			CostUSD: cost, Basis: *snapshot.Basis, Quota: quota}
		encoded, err := common.Marshal(receipt)
		if err != nil {
			return err
		}
		if job.FinancialDetails != "" {
			if job.FinancialDetails != string(encoded) {
				return domain.ErrBatchConflict
			}
			return nil
		}
		if job.ChargedQuota != 0 || job.SettledCount != 0 || job.RequestCount <= 0 {
			return domain.ErrBatchConflict
		}
		var conflicting int64
		if err := tx.Model(&BatchItem{}).Where("batch_id = ? AND (charged_quota <> 0 OR financial_details <> '' OR billing_status IN ?)",
			batchID, []string{"settled", "settled_at_batch"}).Count(&conflicting).Error; err != nil {
			return err
		}
		if conflicting != 0 {
			return domain.ErrBatchConflict
		}
		var channel model.Channel
		if err := model.LockForUpdate(tx).First(&channel, job.ChannelID).Error; err != nil {
			return err
		}
		if err := domain.ValidateAggregateCharge(domain.Job(job), accounts(user, token), channel.UsedQuota, quota); err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&user).Updates(map[string]any{"quota": gorm.Expr("quota - ?",
			quota), "used_quota": gorm.Expr("used_quota + ?", quota), "request_count": gorm.Expr("request_count + ?",
			job.RequestCount)}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&token).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota - ?",
			quota), "used_quota": gorm.Expr("used_quota + ?", quota), "accessed_time": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		if err := tx.Model(&channel).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
			return err
		}
		if err := tx.Model(&BatchItem{}).Where("batch_id = ?", batchID).Update("billing_status", "settled_at_batch").Error; err != nil {
			return err
		}
		if err := tx.Model(&job).Updates(map[string]any{"charged_quota": quota, "settled_count": job.RequestCount,
			"financial_details": string(encoded), "billing_status": "pending_cache"}).Error; err != nil {
			return err
		}
		if common.LogConsumeEnabled {
			basis := billingdomain.ProviderBasis{Version: snapshot.Basis.Version, EffectiveGroupRatio: snapshot.Basis.EffectiveGroupRatio,
				QuotaPerUnit: snapshot.Basis.QuotaPerUnit, EstimatedUSD: snapshot.Basis.EstimatedUSD}
			payload, err := s.logs.Aggregate(domain.Job(job), domain.LogAccount{UserID: user.Id,
				Username: user.Username, TokenID: token.Id, TokenName: token.Name}, quota, cost,
				basis)
			if err != nil {
				return err
			}
			if err := tx.Create(&BatchLogEvent{BatchID: &job.ID, Payload: payload, State: "pending"}).Error; err != nil {
				return err
			}
		}
		return s.saveBatchAccountEvents(tx, &user, &token, batchID, 0, int64(quota))
	})
	// Database/cache infrastructure failures remain retryable. Only invalid
	// monetary evidence or an immutable-receipt conflict requires intervention.
	if errors.Is(err, domain.ErrBatchConflict) {
		marked := s.db().Model(&BatchJob{}).Where("id = ?", batchID).Update("billing_status", "reconciliation_required").Error
		return errors.Join(err, marked)
	}
	return err
}

func accounts(user model.User, token model.Token) domain.Accounts {
	return domain.Accounts{UserID: user.Id, TokenID: token.Id, TokenUserID: token.UserId,
		UserQuota: user.Quota, UserUsed: user.UsedQuota, UserRequests: user.RequestCount, TokenQuota: token.RemainQuota,
		TokenUsed: token.UsedQuota, UserHeld: user.BatchHeldQuota, TokenHeld: token.BatchHeldQuota,
		UserDeleted: user.DeletedAt.Valid, TokenDeleted: token.DeletedAt.Valid, UserStatus: user.Status,
		TokenStatus: token.Status, TokenExpires: token.ExpiredTime}
}
