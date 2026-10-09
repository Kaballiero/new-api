package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// BatchJob owns the asynchronous operation independently of channel plugin
// tasks. Execution and financial reconciliation have separate durable states.
type BatchJob struct {
	ID                int64  `json:"-"`
	PublicID          string `json:"id" gorm:"size:64;not null;uniqueIndex"`
	UserID            int    `json:"-" gorm:"index;not null;uniqueIndex:uk_batch_owner_key,priority:1"`
	TokenID           int    `json:"-" gorm:"index;not null"`
	IdempotencyKey    string `json:"-" gorm:"size:64;not null;uniqueIndex:uk_batch_owner_key,priority:2"`
	RequestHash       string `json:"-" gorm:"size:64;not null"`
	ChannelID         int    `json:"-" gorm:"index;not null"`
	ChannelType       int    `json:"-"`
	CredentialIndex   int    `json:"-"`
	CredentialHash    string `json:"-" gorm:"size:64"`
	ChannelConfigHash string `json:"-" gorm:"size:64"`
	Model             string `json:"model" gorm:"size:255;not null"`
	UpstreamModel     string `json:"-" gorm:"size:255;not null"`
	UsingGroup        string `json:"-" gorm:"size:64"`
	Endpoint          string `json:"endpoint" gorm:"size:64;not null"`
	Status            string `json:"status" gorm:"size:32;index;not null"`
	PurchaseUSD       string `json:"-" gorm:"size:128"`
	PurchaseEvidence  string `json:"-" gorm:"size:4096"`
	PurchaseStatus    string `json:"purchase_status" gorm:"size:32;index;not null"`
	BillingStatus     string `json:"billing_status" gorm:"size:32;index;not null"`
	SubmitState       string `json:"-" gorm:"size:32;index;not null"`
	UpstreamID        string `json:"-" gorm:"size:191"`
	InputFileID       string `json:"-" gorm:"size:191"`
	OutputFileID      string `json:"-" gorm:"size:191"`
	ErrorFileID       string `json:"-" gorm:"size:191"`
	BillingSnapshot   string `json:"-" gorm:"size:1048576;not null"`
	UpstreamUsage     string `json:"-" gorm:"size:1048576"`
	LastError         string `json:"-" gorm:"size:4096"`
	RequestCount      int    `json:"request_count"`
	ResultCount       int    `json:"result_count"`
	SettledCount      int    `json:"settled_count"`
	ReservedQuota     int64  `json:"reserved_quota"`
	ChargedQuota      int64  `json:"charged_quota"`
	CreatedAt         int64  `json:"created_at" gorm:"index"`
	UpdatedAt         int64  `json:"updated_at"`
	ExpiresAt         int64  `json:"expires_at" gorm:"index"`
	DeletedAt         int64  `json:"-" gorm:"index;not null;default:0"`
	CleanupDone       bool   `json:"-" gorm:"not null"`
	LeaseUntil        int64  `json:"-" gorm:"index;not null;default:0"`
	LeaseOwner        string `json:"-" gorm:"size:64"`
}

type BatchItem struct {
	ID               int64  `json:"-"`
	BatchID          int64  `json:"-" gorm:"not null;uniqueIndex:uk_batch_item,priority:1"`
	CustomID         string `json:"custom_id" gorm:"size:64;not null"`
	ProviderCustomID string `json:"-" gorm:"size:64;not null;index"`
	CustomIDHash     string `json:"-" gorm:"size:64;not null;uniqueIndex:uk_batch_item,priority:2"`
	Ordinal          int    `json:"-"`
	Body             string `json:"-" gorm:"size:8388608;not null"`
	UpstreamBody     string `json:"-" gorm:"size:8388608;not null"`
	Result           string `json:"-" gorm:"size:8388608"`
	ResultHash       string `json:"-" gorm:"size:64"`
	PurchaseStatus   string `json:"purchase_status" gorm:"size:32;index;not null"`
	BillingStatus    string `json:"billing_status" gorm:"size:32;not null"`
	ReservedQuota    int64  `json:"reserved_quota"`
	ChargedQuota     int64  `json:"charged_quota"`
	CachePending     bool   `json:"-" gorm:"not null"`
	FinancialDetails string `json:"-" gorm:"size:1048576"`
}

// BatchQuotaEvent is an outbox for cache effects, not a second wallet ledger.
// Wallet/token changes and each event are committed in the same transaction.
// Version is serialized by the account row and prevents duplicate cache debit.
type BatchQuotaEvent struct {
	ID         int64
	OwnerKind  string `gorm:"size:8;not null;uniqueIndex:uk_batch_quota_version,priority:1"`
	OwnerID    int    `gorm:"not null;uniqueIndex:uk_batch_quota_version,priority:2"`
	Version    int64  `gorm:"not null;uniqueIndex:uk_batch_quota_version,priority:3"`
	DebitQuota int64  `gorm:"not null"`
	HeldQuota  int64  `gorm:"not null"`
	BatchID    int64  `gorm:"index;not null"`
	ItemID     int64  `gorm:"index"`
	Applied    bool   `gorm:"index;not null"`
}

var ErrBatchInsufficientQuota = errors.New("insufficient quota for batch reservation")
var ErrBatchConflict = errors.New("batch state conflict")

func GetBatchJob(userID int, publicID string) (*BatchJob, error) {
	var job BatchJob
	err := DB.Where("user_id = ? AND public_id = ? AND deleted_at = 0", userID, publicID).First(&job).Error
	return &job, err
}

func ListBatchJobs(userID int, before int64, limit int) ([]BatchJob, error) {
	var jobs []BatchJob
	query := DB.Where("user_id = ? AND deleted_at = 0", userID)
	if before > 0 {
		query = query.Where("id < ?", before)
	}
	err := query.Order("id desc").Limit(min(max(limit, 1), 101)).Find(&jobs).Error
	return jobs, err
}

// CreateBatchReservation holds quota without withdrawing money. Synchronous
// pre-consume paths subtract these holds from available quota as well. A paid
// upstream create is allowed only after both cache events are acknowledged.
func CreateBatchReservation(job *BatchJob, items []BatchItem) error {
	if len(items) == 0 || job.ReservedQuota < 0 || job.ReservedQuota > common.MaxWalletQuota {
		return errors.New("invalid batch reservation")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, job.UserID).Error; err != nil {
			return err
		}
		var token Token
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", job.TokenID, job.UserID).First(&token).Error; err != nil {
			return err
		}
		if user.BatchHeldQuota < 0 || user.BatchHeldQuota > common.MaxWalletQuota-job.ReservedQuota || int64(user.Quota)-user.BatchHeldQuota < job.ReservedQuota {
			return ErrBatchInsufficientQuota
		}
		if token.BatchHeldQuota < 0 || token.BatchHeldQuota > common.MaxWalletQuota-job.ReservedQuota || (!token.UnlimitedQuota && int64(token.RemainQuota)-token.BatchHeldQuota < job.ReservedQuota) {
			return ErrBatchInsufficientQuota
		}
		var sum int64
		for _, item := range items {
			if item.ReservedQuota < 0 || item.ReservedQuota > common.MaxQuota || sum > job.ReservedQuota-item.ReservedQuota {
				return errors.New("invalid item reservation")
			}
			sum += item.ReservedQuota
		}
		if sum != job.ReservedQuota {
			return errors.New("batch reservation does not equal its item reservations")
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
		return saveBatchAccountEvents(tx, &user, &token, job.ID, 0, 0)
	})
}

// saveBatchAccountEvents writes the two account versions while their locks are
// held. An event's hold is an absolute value; its debit is applied once.
func saveBatchAccountEvents(tx *gorm.DB, user *User, token *Token, batchID, itemID, debit int64) error {
	user.BatchQuotaVersion++
	token.BatchQuotaVersion++
	if user.BatchQuotaVersion <= 0 || token.BatchQuotaVersion <= 0 || user.BatchQuotaVersion > common.MaxWalletQuota || token.BatchQuotaVersion > common.MaxWalletQuota {
		return errors.New("batch quota version overflow")
	}
	if err := tx.Unscoped().Model(user).Updates(map[string]any{"batch_held_quota": user.BatchHeldQuota, "batch_quota_version": user.BatchQuotaVersion}).Error; err != nil {
		return err
	}
	if err := tx.Unscoped().Model(token).Updates(map[string]any{"batch_held_quota": token.BatchHeldQuota, "batch_quota_version": token.BatchQuotaVersion}).Error; err != nil {
		return err
	}
	events := []BatchQuotaEvent{
		{OwnerKind: "user", OwnerID: user.Id, Version: user.BatchQuotaVersion, DebitQuota: debit, HeldQuota: user.BatchHeldQuota, BatchID: batchID, ItemID: itemID, Applied: !common.RedisEnabled},
		{OwnerKind: "token", OwnerID: token.Id, Version: token.BatchQuotaVersion, DebitQuota: debit, HeldQuota: token.BatchHeldQuota, BatchID: batchID, ItemID: itemID, Applied: !common.RedisEnabled},
	}
	return tx.Create(&events).Error
}

// ClaimBatchJob leases local work. Once POST is started the submit state must
// be made submission_unknown before network I/O; lease expiry never re-POSTs it.
func ClaimBatchJob(id int64, owner string) (bool, error) {
	now := time.Now().Unix()
	result := DB.Model(&BatchJob{}).Where("id = ? AND lease_until < ?", id, now).Updates(map[string]any{"lease_owner": owner, "lease_until": now + 120})
	return result.RowsAffected == 1, result.Error
}

func StoreBatchResult(batchID int64, customID, result, hash string) error {
	if len(result) > 8<<20 {
		return errors.New("batch result exceeds storage limit")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var job BatchJob
		if err := lockForUpdate(tx).First(&job, batchID).Error; err != nil {
			return err
		}
		var item BatchItem
		if err := lockForUpdate(tx).Where("batch_id = ? AND custom_id_hash = ?", batchID, fmt.Sprintf("%x", sha256.Sum256([]byte(customID)))).First(&item).Error; err != nil {
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
