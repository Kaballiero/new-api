package domain

import "slices"

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
