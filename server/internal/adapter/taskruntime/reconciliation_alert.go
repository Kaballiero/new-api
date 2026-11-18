package taskruntime

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// ManualHoldsNotification phrases new OpenRouter holds parked in manual
// reconciliation in lang, with the held amounts in USD. At most 12 holds are
// listed with their task id, action and manual reason, or settlement_pending
// for a settlement still retried.
func ManualHoldsNotification(lang string, holds []*domain.Task) (subject, content string) {
	var reservedQuota int
	for _, task := range holds {
		reservedQuota += task.OpenRouterSyncHeldQuota()
	}
	shown := min(len(holds), 12)
	var builder strings.Builder
	builder.WriteString(i18n.Translate(lang, i18n.MsgTasksSyncManualHolds, map[string]any{
		"Count":    len(holds),
		"Reserved": fmt.Sprintf("$%.6f", float64(reservedQuota)/common.QuotaPerUnit),
		"Shown":    shown,
	}))
	for _, task := range holds[:shown] {
		reason := strings.TrimPrefix(task.FailReason, domain.SyncManualFailReason(""))
		if task.PrivateData.Reconciliation.SettlementPending {
			reason = domain.SyncBillingSettlementPending
		}
		builder.WriteString(fmt.Sprintf("\n- %s (%s, %s): $%.6f", task.TaskID, task.Action, reason, float64(task.OpenRouterSyncHeldQuota())/common.QuotaPerUnit))
	}
	return i18n.Translate(lang, i18n.MsgTasksSyncManualHoldsSubject), builder.String()
}

// EstimateSettlementsNotification phrases the OpenRouter results charged an
// estimate of their missing upstream cost in lang, with the charged amounts in
// USD, and the estimate claims that need repair because their balances never
// moved. Each part is omitted when empty; at most 12 charges are listed with
// their task id, action and amount, and at most 12 claims with their task id.
func EstimateSettlementsNotification(lang string, charges, stalledClaims []*domain.Task) (subject, content string) {
	var builder strings.Builder
	if len(charges) > 0 {
		var chargedQuota int
		for _, task := range charges {
			chargedQuota += task.PrivateData.Reconciliation.TargetQuota
		}
		shown := min(len(charges), 12)
		builder.WriteString(i18n.Translate(lang, i18n.MsgTasksSyncEstimateSettlements, map[string]any{
			"Count":   len(charges),
			"Charged": fmt.Sprintf("$%.6f", float64(chargedQuota)/common.QuotaPerUnit),
			"Shown":   shown,
		}))
		for _, task := range charges[:shown] {
			builder.WriteString(fmt.Sprintf("\n- %s (%s): $%.6f", task.TaskID, task.Action, float64(task.PrivateData.Reconciliation.TargetQuota)/common.QuotaPerUnit))
		}
	}
	if len(stalledClaims) > 0 {
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		shown := min(len(stalledClaims), 12)
		taskIDs := make([]string, shown)
		for i, task := range stalledClaims[:shown] {
			taskIDs[i] = task.TaskID
		}
		builder.WriteString(i18n.Translate(lang, i18n.MsgTasksSyncStalledEstimateSettlements, map[string]any{
			"Count":   len(stalledClaims),
			"Shown":   shown,
			"TaskIDs": strings.Join(taskIDs, ", "),
		}))
	}
	return i18n.Translate(lang, i18n.MsgTasksSyncEstimateSettlementsSubject), builder.String()
}
