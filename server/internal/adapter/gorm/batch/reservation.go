package batch

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/batch/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/model"
)

func (s Store) GetBatchJob(userID int, publicID string) (*domain.Job, error) {
	var job BatchJob
	err := s.db().Where("user_id = ? AND public_id = ? AND deleted_at = 0", userID, publicID).First(&job).Error
	out := domain.Job(job)
	return &out, notFound(err)
}

func (s Store) ListBatchJobs(userID int, before int64, limit int) ([]domain.Job, error) {
	var jobs []BatchJob
	query := s.db().Where("user_id = ? AND deleted_at = 0", userID)
	if before > 0 {
		query = query.Where("id < ?", before)
	}
	err := query.Order("id desc").Limit(min(max(limit, 1), 101)).Find(&jobs).Error
	var out []domain.Job
	if jobs != nil {
		out = make([]domain.Job, len(jobs))
		for i := range jobs {
			out[i] = domain.Job(jobs[i])
		}
	}
	return out, err
}

// CreateBatchReservation holds quota without withdrawing money. Synchronous
// pre-consume paths subtract these holds from available quota as well. A paid
// upstream create is allowed only after both cache events are acknowledged.
func (s Store) CreateBatchReservation(value *domain.Job, values []domain.Item) error {
	job := record.BatchJob(*value)
	items := make([]record.BatchItem, len(values))
	for i := range values {
		items[i] = record.BatchItem(values[i])
	}
	defer func() {
		*value = domain.Job(job)
		for i := range items {
			values[i] = domain.Item(items[i])
		}
	}()
	return s.createReservation(&job, items)
}
func (s Store) createReservation(job *BatchJob, items []BatchItem) error {
	if err := domain.ValidateReservationInput(len(items), job.ReservedQuota, common.MaxWalletQuota); err != nil {
		return err
	}
	return s.db().Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := locking.LockForUpdate(tx).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token model.Token
		if err := locking.LockForUpdate(tx).Where("id = ? AND user_id = ?", job.TokenID, job.UserID).First(&token).Error; err != nil {
			return err
		}
		quotas := make([]int64, len(items))
		for i := range items {
			quotas[i] = items[i].ReservedQuota
		}
		if err := domain.ValidateReservationAccounts(domain.Job(*job), accounts(user, token),
			token.UnlimitedQuota, quotas, common.MaxWalletQuota, common.MaxQuota); err != nil {
			return err
		}
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].BatchID = job.ID
			items[i].ProviderCustomID = fmt.Sprintf("%x", sha256.Sum256([]byte(job.PublicID+":"+items[i].CustomID)))
			items[i].CustomIDHash = fmt.Sprintf("%x", sha256.Sum256([]byte(items[i].CustomID)))
		}
		if err := tx.CreateInBatches(items, 50).Error; err != nil {
			return err
		}
		user.BatchHeldQuota += job.ReservedQuota
		token.BatchHeldQuota += job.ReservedQuota
		return s.saveBatchAccountEvents(tx, &user, &token, job.ID, 0, 0)
	})
}

// saveBatchAccountEvents writes the two account versions while their locks are
// held. An event's hold is an absolute value; its debit is applied once.
func (s Store) saveBatchAccountEvents(tx *gorm.DB, user *model.User, token *model.Token, batchID, itemID, debit int64) error {
	user.BatchQuotaVersion++
	token.BatchQuotaVersion++
	if user.BatchQuotaVersion <= 0 || token.BatchQuotaVersion <= 0 || user.BatchQuotaVersion > common.MaxWalletQuota ||
		token.BatchQuotaVersion > common.MaxWalletQuota {
		return errors.New("batch quota version overflow")
	}
	if err := tx.Unscoped().Model(user).Updates(map[string]any{"batch_held_quota": user.BatchHeldQuota,
		"batch_quota_version": user.BatchQuotaVersion}).Error; err != nil {
		return err
	}
	if err := tx.Unscoped().Model(token).Updates(map[string]any{"batch_held_quota": token.BatchHeldQuota,
		"batch_quota_version": token.BatchQuotaVersion}).Error; err != nil {
		return err
	}
	events := []BatchQuotaEvent{
		{OwnerKind: "user", OwnerID: user.Id, Version: user.BatchQuotaVersion, DebitQuota: debit,
			HeldQuota: user.BatchHeldQuota, BatchID: batchID, ItemID: itemID, Applied: !common.RedisEnabled},

		{OwnerKind: "token", OwnerID: token.Id, Version: token.BatchQuotaVersion, DebitQuota: debit,
			HeldQuota: token.BatchHeldQuota, BatchID: batchID, ItemID: itemID, Applied: !common.RedisEnabled},
	}
	return tx.Create(&events).Error
}

// ClaimBatchJob leases local work. Once POST is started the submit state must
// be made submission_unknown before network I/O; lease expiry never re-POSTs it.
func (s Store) ClaimBatchJob(id int64, owner string) (bool, error) {
	now := time.Now().Unix()
	result := s.db().Model(&BatchJob{}).Where("id = ? AND lease_until < ?", id, now).Updates(map[string]any{"lease_owner": owner,
		"lease_until": now + 120})
	return result.RowsAffected == 1, result.Error
}

func (s Store) StoreBatchResult(batchID int64, customID, result, hash string) error {
	if len(result) > 8<<20 {
		return errors.New("batch result exceeds storage limit")
	}
	return s.db().Transaction(func(tx *gorm.DB) error {
		var job BatchJob
		if err := locking.LockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var item BatchItem
		if err := locking.LockForUpdate(tx).Where("batch_id = ? AND custom_id_hash = ?", batchID,
			fmt.Sprintf("%x", sha256.Sum256([]byte(customID)))).First(&item).Error; err != nil {
			return err
		}
		if item.ResultHash != "" {
			if item.ResultHash != hash {
				return fmt.Errorf("conflicting result for custom_id %q", customID)
			}
			return nil
		}
		if err := tx.Model(&item).Updates(map[string]any{"result": result, "result_hash": hash}).Error; err != nil {
			return err
		}
		return tx.Model(&BatchJob{}).Where("id = ?", batchID).Update("result_count", gorm.Expr("result_count + 1")).Error
	})
}
