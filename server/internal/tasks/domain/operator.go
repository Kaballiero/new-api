package domain

import (
	"fmt"
	"slices"
	"time"
)

// openRouterSyncUndeliveredReasons are result reasons recorded before any
// output reached the client, so a zero-cost release cannot hide a delivery.
var openRouterSyncUndeliveredReasons = []string{"submission_outcome_unknown", "undelivered_chat_result", "chat_image_upstream_error"}

// OpenRouterSyncOperatorEligible reports a durable OpenRouter hold parked in
// manual reconciliation without an upstream cost, which root may resolve by a
// zero-cost release (nothing delivered) or an explicit cost settlement.
func (t *Task) OpenRouterSyncOperatorEligible(action string) bool {
	a, r, bc := t.PrivateData.OpenRouterSyncState(), t.PrivateData.Reconciliation, t.PrivateData.BillingContext
	var operation bool
	switch t.Platform {
	case string(TaskPlatformOpenRouterChatImage):
		operation = t.Action == "chat-image" || t.Action == "chat"
	case string(TaskPlatformOpenRouterAudio):
		operation = t.Action == "audio"
	case string(TaskPlatformOpenRouterImage):
		operation = t.Action == "image"
	case string(TaskPlatformOpenRouterRerank):
		operation = t.Action == "rerank"
	}
	if !operation || t.Status != TaskStatusFailure || a == nil || r == nil || bc == nil ||
		bc.SettlementMode != TaskSettlementOpenRouterCostV2 || bc.OpenRouterCost == nil || bc.OpenRouterCost.Version != TaskSettlementOpenRouterCostV2 ||
		!r.Required || r.ReservationPending || r.SubmissionPending || r.SettlementPending || r.MainApplied || r.Finalized ||
		r.HasUpstreamCost || a.OperatorDecision != nil {
		return false
	}
	switch action {
	case OpenRouterSyncOperatorRelease:
		// Legacy tasks predate ResultReason; an unready result is their evidence.
		// Chat text tasks deliver before Complete and never predate tracking.
		legacy := a.ResultReason == "" && !a.ResultReasonTracked && t.Action != "chat"
		return r.ReservedQuota > 0 && !a.IdentityConflict && !a.ResultReady &&
			(legacy || slices.Contains(openRouterSyncUndeliveredReasons, a.ResultReason))
	case OpenRouterSyncOperatorSettle:
		return r.ReservedQuota >= 0
	}
	return false
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
