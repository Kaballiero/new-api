package domain

// SyncReasonEstimateSettled is the reason code of a manual hold settled at an
// estimate of its missing upstream cost.
const SyncReasonEstimateSettled = "estimate_settled"

// OpenRouterReserveEstimateUSD is the reserve of an OpenRouter task, the
// estimate of a delivered result whose price is the reserve itself.
func OpenRouterReserveEstimateUSD(task *Task) float64 {
	return task.PrivateData.BillingContext.OpenRouterCost.EstimatedUSD
}

// OpenRouterSyncEstimateUSD is the estimate a delivered manual hold without an
// upstream cost settles at, or zero when the hold stays with root. A text
// whose cost lookup ran out is charged its usage at the reserve tariffs, never
// above the reserve; a native image is charged its reserve unless its endpoint
// has no published price.
func OpenRouterSyncEstimateUSD(task *Task) float64 {
	a := task.PrivateData.OpenRouterSyncState()
	if !a.ResultReady || a.ResultFailed || a.IdentityConflict {
		return 0
	}
	switch {
	case task.Platform == string(TaskPlatformOpenRouterImage) && a.LastError == "missing_generation_identity" && !a.PricingUnknown:
		return OpenRouterReserveEstimateUSD(task)
	case task.Platform == string(TaskPlatformOpenRouterChatImage) && a.LastError == "cost_lookup_deadline_or_attempt_limit":
		return min(a.UsageEstimateUSD, OpenRouterReserveEstimateUSD(task))
	}
	return 0
}

// RecordOpenRouterCostEstimate records an estimate as the cost a settlement
// prices, marked estimated for the ledger and the operators.
func RecordOpenRouterCostEstimate(task *Task, costUSD float64) {
	r := task.PrivateData.Reconciliation
	r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD, r.UpstreamCostEstimated = true, true, costUSD, true
}
