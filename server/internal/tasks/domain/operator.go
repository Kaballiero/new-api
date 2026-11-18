package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// openRouterSyncUndeliveredReasons are result reasons recorded before any
// output reached the client, so a zero-cost release cannot hide a delivery.
var openRouterSyncUndeliveredReasons = []string{"submission_outcome_unknown", "undelivered_chat_result", "chat_image_upstream_error"}

// OpenRouterSyncOperatorEligible reports a durable OpenRouter hold parked in
// manual reconciliation without an upstream cost, which root may resolve by a
// zero-cost release (nothing delivered) or an explicit cost settlement, or an
// uncommitted reservation that may hold no quota, which root may release.
func (t *Task) OpenRouterSyncOperatorEligible(action string) bool {
	a, r, bc := t.PrivateData.OpenRouterSyncState(), t.PrivateData.Reconciliation, t.PrivateData.BillingContext
	var operation bool
	switch t.Platform {
	case string(TaskPlatformOpenRouterChatImage):
		operation = IsOpenRouterChatImageAction(t.Action)
	case string(TaskPlatformOpenRouterAudio):
		operation = t.Action == TaskActionAudio
	case string(TaskPlatformOpenRouterImage):
		operation = t.Action == TaskActionImage
	case string(TaskPlatformOpenRouterRerank):
		operation = t.Action == TaskActionRerank
	}
	if !operation || t.Status != TaskStatusFailure || a == nil || r == nil ||
		!r.Required || r.SubmissionPending || r.SettlementPending || r.MainApplied || r.Finalized ||
		r.HasUpstreamCost || a.OperatorDecision != nil {
		return false
	}
	// A reservation never committed was never submitted. One that may hold no
	// quota is closed by a release without a settlement; any other stays with
	// root until its funds are checked.
	if r.ReservationPending {
		return action == OpenRouterSyncOperatorRelease && t.OpenRouterSyncHeldQuota() == 0
	}
	if bc == nil || bc.SettlementMode != TaskSettlementOpenRouterCostV2 || bc.OpenRouterCost == nil || bc.OpenRouterCost.Version != TaskSettlementOpenRouterCostV2 {
		return false
	}
	switch action {
	case OpenRouterSyncOperatorRelease:
		// Legacy tasks predate ResultReason; an unready result is their evidence.
		// Chat text tasks deliver before Complete and never predate tracking.
		legacy := a.ResultReason == "" && !a.ResultReasonTracked && t.Action != TaskActionChat
		return r.ReservedQuota > 0 && !a.IdentityConflict && !a.ResultReady &&
			(legacy || slices.Contains(openRouterSyncUndeliveredReasons, a.ResultReason))
	case OpenRouterSyncOperatorSettle:
		return r.ReservedQuota >= 0
	}
	return false
}

// OpenRouterSyncHeldQuota is the quota a hold may keep: the recorded reserve,
// or for a reservation not yet committed the larger of it and the planned
// reserve, which the billing session may take before Commit records it. A
// zero planned reserve funded by a subscription still takes one quota unit.
func (t *Task) OpenRouterSyncHeldQuota() int {
	r := t.PrivateData.Reconciliation
	if !r.ReservationPending {
		return r.ReservedQuota
	}
	return max(r.ReservedQuota, t.PrivateData.OpenRouterSyncState().PlannedQuota)
}

// OpenRouterSyncSettlementRetryable reports a terminal result whose settlement
// of a confirmed cost is pending without a recorded decision and moved no
// balances. Root repeats that frozen settlement by asserting the confirmed
// cost. A manual hold or an unverified generation identity is not a result.
func (t *Task) OpenRouterSyncSettlementRetryable() bool {
	a, r := t.PrivateData.OpenRouterSyncState(), t.PrivateData.Reconciliation
	terminal := t.Status == TaskStatusSuccess || t.Status == TaskStatusFailure
	manualHold := strings.HasPrefix(t.FailReason, SyncManualFailReason(""))
	return terminal && !manualHold && a != nil && r != nil && !a.IdentityConflict && a.OperatorDecision == nil &&
		r.HasUpstreamCost && r.UpstreamCostConfirmed && r.SettlementPending && !r.MainApplied && !r.Finalized
}

// OpenRouterSyncAutoReleaseDue reports a hold root could release whose
// submission outcome stayed unknown without an upstream identity for the
// auto-release delay. A Task with an identity keeps the cost lookup flow.
func (t *Task) OpenRouterSyncAutoReleaseDue(now time.Time) bool {
	return t.OpenRouterSyncOperatorEligible(OpenRouterSyncOperatorRelease) &&
		t.PrivateData.OpenRouterSyncState().ResultReason == SyncReasonSubmissionOutcomeUnknown &&
		t.PrivateData.UpstreamTaskID == "" &&
		!now.Before(time.Unix(t.SubmitTime, 0).Add(OpenRouterSyncAutoReleaseDelay))
}

// PriceOpenRouterSyncDecision confirms the cost a decision asserts and prices
// it on the frozen basis; a decision that cannot settle exactly is refused.
func (t *Task) PriceOpenRouterSyncDecision(costUSD float64, prepare func(*Task)) error {
	r := t.PrivateData.Reconciliation
	r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, costUSD
	prepare(t)
	if r.QuotaSaturation != "" {
		return fmt.Errorf("%w: cost saturates the quota range", ErrOpenRouterSyncOperatorInvalid)
	}
	if !r.SettlementPending {
		return fmt.Errorf("%w: settlement basis is unavailable", ErrOpenRouterSyncOperatorConflict)
	}
	return nil
}

// RecordOpenRouterSyncDecision stores the irreversible decision with its
// public reason code; the revision claim then admits exactly this decision.
func (t *Task) RecordOpenRouterSyncDecision(decision OpenRouterSyncOperatorDecision, reasonCode, failReason string) {
	a := t.PrivateData.OpenRouterSyncState()
	a.OperatorDecision, a.LastError = &decision, reasonCode
	t.FailReason = failReason
}
