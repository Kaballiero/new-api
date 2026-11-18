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
// listed with their task id, action and manual reason.
func ManualHoldsNotification(lang string, holds []*domain.Task) (subject, content string) {
	var reservedQuota int
	for _, task := range holds {
		reservedQuota += task.PrivateData.Reconciliation.ReservedQuota
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
		builder.WriteString(fmt.Sprintf("\n- %s (%s, %s): $%.6f", task.TaskID, task.Action, reason, float64(task.PrivateData.Reconciliation.ReservedQuota)/common.QuotaPerUnit))
	}
	return i18n.Translate(lang, i18n.MsgTasksSyncManualHoldsSubject), builder.String()
}

// EstimateSettlementsNotification phrases the OpenRouter results charged an
// estimate of their missing upstream cost in lang, with the charged amounts in
// USD. At most 12 charges are listed with their task id, action and amount.
func EstimateSettlementsNotification(lang string, tasks []*domain.Task) (subject, content string) {
	var chargedQuota int
	for _, task := range tasks {
		chargedQuota += task.PrivateData.Reconciliation.TargetQuota
	}
	shown := min(len(tasks), 12)
	var builder strings.Builder
	builder.WriteString(i18n.Translate(lang, i18n.MsgTasksSyncEstimateSettlements, map[string]any{
		"Count":   len(tasks),
		"Charged": fmt.Sprintf("$%.6f", float64(chargedQuota)/common.QuotaPerUnit),
		"Shown":   shown,
	}))
	for _, task := range tasks[:shown] {
		builder.WriteString(fmt.Sprintf("\n- %s (%s): $%.6f", task.TaskID, task.Action, float64(task.PrivateData.Reconciliation.TargetQuota)/common.QuotaPerUnit))
	}
	return i18n.Translate(lang, i18n.MsgTasksSyncEstimateSettlementsSubject), builder.String()
}
