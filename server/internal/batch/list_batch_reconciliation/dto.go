package list_batch_reconciliation

import "github.com/QuantumNous/new-api/internal/batch/domain"

type Entry struct {
	ID             string          `json:"id"`
	UserID         int             `json:"user_id"`
	TokenID        int             `json:"token_id"`
	ChannelID      int             `json:"channel_id"`
	Model          string          `json:"model"`
	UpstreamModel  string          `json:"upstream_model"`
	Status         string          `json:"status"`
	SubmitState    string          `json:"submit_state"`
	BillingStatus  string          `json:"billing_status"`
	PurchaseStatus string          `json:"purchase_status"`
	UpstreamID     string          `json:"upstream_id"`
	RequestCount   int             `json:"request_count"`
	ResultCount    int             `json:"result_count"`
	SettledCount   int             `json:"settled_count"`
	ReservedQuota  int64           `json:"reserved_quota"`
	ChargedQuota   int64           `json:"charged_quota"`
	CreatedAt      int64           `json:"created_at"`
	UpdatedAt      int64           `json:"updated_at"`
	Diagnostic     *domain.Failure `json:"diagnostic,omitempty"`
	Reason         string          `json:"reason"`
	CanRelease     bool            `json:"can_release"`
	CanRecover     bool            `json:"can_recover"`
}
