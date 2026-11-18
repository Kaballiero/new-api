package domain

import (
	"slices"

	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
)

// SyncReasonEstimateSettled is the reason code of a manual hold settled at an
// estimate of its missing upstream cost.
const SyncReasonEstimateSettled = "estimate_settled"

// Stored failure texts of a manual hold settled at an estimate: the claim
// records the pending text, which the settlement apply switches to the
// settled one with the balances. The retry selects the pending holds, the
// estimate summary the settled ones.
const (
	SyncEstimatePendingFailReason = "OpenRouter cost settlement by estimate pending"
	SyncEstimateSettledFailReason = "OpenRouter cost settled by estimate"
)

// openRouterVideoTariffSources are the reserve sources of a video reserve
// priced from the endpoint tariffs alone, the only reserve a delivered video
// without its cost is charged.
var openRouterVideoTariffSources = []pricingdomain.ReserveSource{pricingdomain.ReserveSourceCatalog, pricingdomain.ReserveSourceCatalogStale, pricingdomain.ReserveSourceLive}

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

// OpenRouterVideoEstimateUSD is the estimate a delivered video without its
// cost settles at: its reserve when the endpoint tariffs alone priced it, or
// zero when a channel reserve, a configured price or the approximate source
// media reserve set it and the video stays with root.
func OpenRouterVideoEstimateUSD(task *Task) float64 {
	bc := task.PrivateData.BillingContext
	if bc.OpenRouterCost == nil {
		return 0
	}
	source, _ := bc.OpenRouterOrderFacts["reservation_source"].(string)
	if !slices.Contains(openRouterVideoTariffSources, pricingdomain.ReserveSource(source)) {
		return 0
	}
	return OpenRouterReserveEstimateUSD(task)
}

// RecordOpenRouterCostEstimate records an estimate as the cost a settlement
// prices, marked estimated for the ledger and the operators.
func RecordOpenRouterCostEstimate(task *Task, costUSD float64) {
	r := task.PrivateData.Reconciliation
	r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD, r.UpstreamCostEstimated = true, true, costUSD, true
}
