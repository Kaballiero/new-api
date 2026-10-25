// Package record preserves the deployed Batch schema and stored JSON shapes.
package record

import "github.com/QuantumNous/new-api/common"

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
	FinancialDetails  string `json:"-" gorm:"size:1048576"`
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

// BatchFailure contains only normalized provider diagnostics. Arbitrary provider
// messages, response bodies and headers must never be persisted here.
type BatchFailure struct {
	Version    int    `json:"version"`
	Phase      string `json:"phase"`
	Code       string `json:"code"`
	Category   string `json:"category,omitempty"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
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

// BatchLogEvent belongs to the same transaction as its wallet receipt.
type BatchLogEvent struct {
	ID      int64
	ItemID  *int64 `gorm:"uniqueIndex"`
	BatchID *int64 `gorm:"uniqueIndex"`
	Payload string `gorm:"size:1048576;not null"`
	State   string `gorm:"size:32;not null;index"`
}

// BatchLogReceipt lives in the SQL log database. Its unique business key and
// consume log are committed together, including when logs use a separate DB.
type BatchLogReceipt struct {
	RequestID string `gorm:"size:64;primaryKey"`
	LogID     int    `gorm:"not null;default:0"`
}

// BatchJobCostReceipt is the immutable, job-scoped monetary fact. No part of
// this total is attributed to individual output rows.
type BatchJobCostReceipt struct {
	Version    string                     `json:"version"`
	UpstreamID string                     `json:"upstream_id"`
	CostUSD    float64                    `json:"cost_usd"`
	Basis      common.OpenRouterCostBasis `json:"basis"`
	Quota      int                        `json:"quota"`
}
