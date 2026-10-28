package domain

import "time"

const (
	batchProviderWindow    = 24 * time.Hour
	batchProviderGrace     = 2 * time.Hour
	batchUnknownAlertAfter = 48 * time.Hour
)

// SubmissionOverdue reports an unknown creation old enough that the
// provider would have finished it; only an operator may recover or release it.
func SubmissionOverdue(job *Job, now time.Time) bool {
	return job.SubmitState == "submission_unknown" && now.Unix()-job.CreatedAt >= int64(batchUnknownAlertAfter/time.Second)
}

// ProviderWindowExceeded reports a submitted Batch still not terminal
// after the provider completion window plus a grace period. Only adapters that
// request completion_window "24h" have that window. The clock starts at
// CreatedAt: no recovery timestamp is stored, so a Batch recovered late is
// flagged on its first tick after recovery.
func ProviderWindowExceeded(job *Job, now time.Time, kinds ChannelKinds) bool {
	if job.ChannelType != kinds.OpenAI && job.ChannelType != kinds.OpenRouter {
		return false
	}
	return job.UpstreamID != "" && !State(job.Status).Terminal() && now.Unix() >= job.CreatedAt+int64((batchProviderWindow+batchProviderGrace)/time.Second)
}

func ReconciliationReason(job *Job, now time.Time, kinds ChannelKinds, codec Codec) string {
	if job.SubmitState == "submission_unknown" {
		if SubmissionOverdue(job, now) {
			return "submission_unknown_overdue"
		}
		return "submission_unknown"
	}
	if job.BillingStatus != "settled" {
		if ProviderWindowExceeded(job, now, kinds) {
			return "provider_window_exceeded"
		}
		var snapshot struct {
			Mode string `json:"settlement_mode"`
		}
		if State(job.Status).Terminal() && job.FinancialDetails == "" && codec.UnmarshalJsonStr(job.BillingSnapshot, &snapshot) == nil && snapshot.Mode == BatchSettlementOpenRouterJobCostV2 {
			return "terminal_without_cost"
		}
		return "unsettled_items"
	}
	return "purchase_unconfirmed"
}

func RecoverEligible(job *Job, supported func(int) bool) bool {
	return job.SubmitState == "submission_unknown" && job.UpstreamID == "" && job.DeletedAt == 0 && supported(job.ChannelType)
}

type ReleaseEligibilityStore interface{ CountTouchedItems(int64) (int64, error) }

func ReleaseEligible(store ReleaseEligibilityStore, job *Job) (bool, error) {
	if job.SubmitState != "submission_unknown" || job.UpstreamID != "" || job.DeletedAt != 0 || State(job.Status).Terminal() || job.ResultCount != 0 || job.SettledCount != 0 || job.ChargedQuota != 0 || job.FinancialDetails != "" || job.PurchaseStatus == "settled" {
		return false, nil
	}
	touched, err := store.CountTouchedItems(job.ID)
	return touched == 0, err
}

const OperatorReleasedCode = "operator_released_unsubmitted"

type ReleaseResult struct {
	AlreadyReleased   bool
	Pending           bool
	ReleasedQuota     int64
	PreviousLastError string
}
