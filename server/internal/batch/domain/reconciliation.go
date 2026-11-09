package domain

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

const (
	batchProviderWindow          = 24 * time.Hour
	batchProviderGrace           = 2 * time.Hour
	batchTerminalZeroSettleAfter = time.Hour
	batchUnknownAutoReleaseAfter = 24 * time.Hour
	batchUnknownAlertAfter       = 48 * time.Hour
)

// AutoReleaseDue reports an unknown creation old enough for the poller to
// release its hold; ReleaseEligible decides whether the release is allowed.
func AutoReleaseDue(job *Job, now time.Time) bool {
	return job.SubmitState == "submission_unknown" && now.Unix()-job.CreatedAt >= int64(batchUnknownAutoReleaseAfter/time.Second)
}

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

// unfinishedTerminalStates are the provider states of a Batch that ended
// without completing its work.
var unfinishedTerminalStates = []State{Failed, Expired, Cancelled}

// TerminalWithoutCostCode marks the diagnostic and the zero receipts of a
// Batch that ended without results or provider cost.
const TerminalWithoutCostCode = "terminal_without_cost"

// ZeroSettleAllowed reports an aggregate-cost Batch the provider ended
// unfinished with no results that may be settled with unbilled zero receipts:
// it has no provider cost, or its zero settlement has already begun, which a
// later cost does not undo. The adapter applies it under the job row lock.
func ZeroSettleAllowed(job Job, cost json.RawMessage) bool {
	endedEmpty := slices.Contains(unfinishedTerminalStates, State(job.Status)) && job.UpstreamID != "" &&
		job.ResultCount == 0 && job.FinancialDetails == ""
	costMissing := len(cost) == 0 || string(cost) == "null"
	zeroSettleBegun := job.SettledCount > 0 && job.ChargedQuota == 0
	return endedEmpty && (costMissing || zeroSettleBegun)
}

// TerminalZeroSettle is the poller's next step for an aggregate-cost Batch
// that may have ended without cost.
type TerminalZeroSettle string

const (
	TerminalZeroSettleNone    TerminalZeroSettle = "none"
	TerminalZeroSettleObserve TerminalZeroSettle = "observe"
	TerminalZeroSettleWait    TerminalZeroSettle = "wait"
	TerminalZeroSettleRun     TerminalZeroSettle = "settle"
	TerminalZeroSettleForget  TerminalZeroSettle = "forget"
)

// ToTerminalZeroSettle decides the step from the stored job, its settlement
// mode and provider cost, and when the poller first observed it ended without
// cost: store that observation, wait an hour, settle at zero, or forget the
// observation once the job no longer qualifies (for example, a cost settled it).
func ToTerminalZeroSettle(job Job, mode string, cost json.RawMessage, observedAt int64, now time.Time) TerminalZeroSettle {
	allowed := mode == BatchSettlementOpenRouterJobCostV2 && ZeroSettleAllowed(job, cost)
	switch {
	case allowed && job.BillingStatus == "settled":
		// The zero settlement is complete; it is never repeated or logged again.
		return TerminalZeroSettleNone
	case !allowed && observedAt != 0:
		return TerminalZeroSettleForget
	case !allowed:
		return TerminalZeroSettleNone
	case job.SettledCount > 0:
		return TerminalZeroSettleRun
	case observedAt == 0:
		return TerminalZeroSettleObserve
	case now.Unix()-observedAt < int64(batchTerminalZeroSettleAfter/time.Second):
		return TerminalZeroSettleWait
	default:
		return TerminalZeroSettleRun
	}
}

// TerminalObservedAt returns when the poller first observed the Batch ended
// without cost, stored in its diagnostic like AlertedAt; zero until observed.
func TerminalObservedAt(job *Job, codec Codec) int64 {
	var failure Failure
	if codec.UnmarshalJsonStr(job.LastError, &failure) != nil {
		return 0
	}
	return failure.TerminalObservedAt
}

// diagnosticSubmitStates are submit states whose stored diagnostic a poll tick
// keeps.
var diagnosticSubmitStates = []string{"submission_unknown", "rejected"}

// KeepsDiagnostic reports a job whose stored diagnostic a poll tick must not
// overwrite: an unknown or rejected submission, or a Batch awaiting its zero
// settlement after the poller observed it ended without cost.
func KeepsDiagnostic(job *Job, codec Codec) bool {
	awaitingZeroSettle := job.BillingStatus != "settled" && TerminalObservedAt(job, codec) != 0
	return slices.Contains(diagnosticSubmitStates, job.SubmitState) || awaitingZeroSettle
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

const (
	OperatorReleasedCode = "operator_released_unsubmitted"
	AutoReleasedCode     = "auto_released_unsubmitted"
)

var releasedUnsubmittedCodes = []string{OperatorReleasedCode, AutoReleasedCode}

// HoldRelease names who releases an unsubmitted hold and the diagnostic it stores.
type HoldRelease struct {
	Actor, Code, Message string
}

func OperatorHoldRelease(operatorID int) HoldRelease {
	return HoldRelease{Actor: fmt.Sprintf("root user %d", operatorID), Code: OperatorReleasedCode, Message: "Operator confirmed no provider Batch exists; hold released."}
}

func AutoHoldRelease() HoldRelease {
	return HoldRelease{Actor: "batch poller", Code: AutoReleasedCode, Message: "No provider Batch identity appeared within the release window; hold released automatically."}
}

type ReleaseResult struct {
	AlreadyReleased   bool
	Pending           bool
	ReleasedQuota     int64
	PreviousLastError string
}
