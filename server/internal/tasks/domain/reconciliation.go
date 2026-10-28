package domain

// OpenRouterSyncReconciliation is an allowlisted public projection. Provider
// identities, hashes, headers and raw diagnostics never leave private state.
func OpenRouterSyncReconciliation(task *Task) map[string]any {
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if a == nil || r == nil {
		return nil
	}
	submission, billing, reason := "unknown", "awaiting_cost", ""
	if a.DispatchPrepared {
		submission = "possible_sent"
	}
	if task.PrivateData.UpstreamTaskID != "" && !a.IdentityConflict {
		submission = "identity_confirmed"
	}
	switch {
	case r.Finalized:
		billing = "settled"
	case r.MainApplied:
		billing = "settlement_repair_required"
	case r.SettlementPending:
		billing = "settlement_pending"
	case r.ReservationPending:
		billing, submission = "reservation_unknown", "preparing"
	case task.Status == TaskStatusFailure:
		billing = "manual_reconciliation"
	}
	switch a.LastError {
	case "missing_generation_identity", "interrupted_reservation_or_submission", "generation_identity_unverified", "cost_lookup_deadline_or_attempt_limit", "auth", "account_binding_changed", "identity_or_invalid_cost", "incomplete_metadata", "temporary_http_error", "transport_error", "transport_unavailable", "permanent_http_error", "submission_outcome_unknown", "upstream_rejected", "operator_released", "operator_settled":
		reason = a.LastError
	default:
		if billing == "manual_reconciliation" {
			reason = "reconciliation_required"
		}
	}
	if a.IdentityConflict {
		submission, reason = "unknown", "generation_identity_unverified"
	}
	var nextLookup any
	if a.NextPoll > 0 && task.Status != TaskStatusFailure && task.Status != TaskStatusSuccess {
		nextLookup = a.NextPoll
	}
	return map[string]any{"submission_state": submission, "billing_state": billing, "reason_code": reason,
		"replay_allowed": false, "identity_available": task.PrivateData.UpstreamTaskID != "" && !a.IdentityConflict,
		"cost_confirmed": r.HasUpstreamCost && r.UpstreamCostConfirmed, "next_lookup_at": nextLookup,
		"result_ready": a.ResultReady, "result_failed": a.ResultFailed}
}

// OpenRouterSyncAdminView extends the public projection with diagnostics an
// administrator needs to resolve a manual hold. Provider identities, hashes,
// headers and operator evidence remain private.
func OpenRouterSyncAdminView(task *Task) map[string]any {
	view := OpenRouterSyncReconciliation(task)
	if view == nil {
		return nil
	}
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	var decision any
	if d := a.OperatorDecision; d != nil {
		decision = map[string]any{"action": d.Action, "cost_usd": d.CostUSD, "operator_id": d.OperatorID, "decided_at": d.DecidedAt}
	}
	view["reserved_quota"] = r.ReservedQuota
	view["target_quota"] = r.TargetQuota
	view["result_reason"] = a.ResultReason
	view["last_lookup_class"] = a.LastLookupClass
	view["last_http_status"] = a.LastHTTPStatus
	view["lookup_attempts"] = a.Attempts
	view["identity_conflict"] = a.IdentityConflict
	view["operator_decision"] = decision
	view["release_allowed"] = task.OpenRouterSyncOperatorEligible(OpenRouterSyncOperatorRelease)
	view["settle_allowed"] = task.OpenRouterSyncOperatorEligible(OpenRouterSyncOperatorSettle)
	return view
}
